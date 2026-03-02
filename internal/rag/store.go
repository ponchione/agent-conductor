package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow/go/v17/arrow"
	"github.com/apache/arrow/go/v17/arrow/array"
	"github.com/apache/arrow/go/v17/arrow/memory"

	"github.com/lancedb/lancedb-go/pkg/contracts"
	"github.com/lancedb/lancedb-go/pkg/lancedb"
)

// SchemaVersion tracks the LanceDB table schema. Bump when columns change
// to trigger a full re-index with DropAndRecreateTable.
const SchemaVersion = "3"

const tableName = "chunks"

// Store wraps LanceDB for storing and retrieving code chunks with embeddings.
type Store struct {
	conn  contracts.IConnection
	table contracts.ITable
	pool  memory.Allocator
}

// NewStore connects to (or creates) a LanceDB database at the given directory
// and opens (or creates) the chunks table.
func NewStore(ctx context.Context, dataDir string) (*Store, error) {
	conn, err := lancedb.Connect(ctx, dataDir, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to lancedb at %s: %w", dataDir, err)
	}

	s := &Store{
		conn: conn,
		pool: memory.NewGoAllocator(),
	}

	table, err := s.openOrCreateTable(ctx)
	if err != nil {
		conn.Close()
		return nil, err
	}
	s.table = table

	return s, nil
}

// openOrCreateTable opens the chunks table if it exists, otherwise creates it.
func (s *Store) openOrCreateTable(ctx context.Context) (contracts.ITable, error) {
	names, err := s.conn.TableNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}

	if slices.Contains(names, tableName) {
		return s.conn.OpenTable(ctx, tableName)
	}

	schema, err := buildSchema()
	if err != nil {
		return nil, fmt.Errorf("failed to build schema: %w", err)
	}

	table, err := s.conn.CreateTable(ctx, tableName, schema)
	if err != nil {
		return nil, fmt.Errorf("failed to create table %s: %w", tableName, err)
	}
	return table, nil
}

// buildSchema constructs the LanceDB table schema matching the Chunk struct.
func buildSchema() (contracts.ISchema, error) {
	return lancedb.NewSchemaBuilder().
		AddStringField("id", false).
		AddStringField("project_name", false).
		AddStringField("file_path", false).
		AddStringField("language", false).
		AddStringField("chunk_type", false).
		AddStringField("name", false).
		AddStringField("signature", true).
		AddStringField("body", true).
		AddStringField("description", true).
		AddInt32Field("line_start", false).
		AddInt32Field("line_end", false).
		AddStringField("content_hash", false).
		AddTimestampField("indexed_at", arrow.Microsecond, false).
		AddStringField("calls", true).
		AddStringField("called_by", true).
		AddStringField("types_used", true).
		AddStringField("implements_ifaces", true).
		AddStringField("imports", true).
		AddVectorField("embedding", DefaultEmbeddingDims, contracts.VectorDataTypeFloat32, false).
		Build()
}

// UpsertChunks writes chunks with their embeddings to LanceDB.
// Existing chunks with the same ID are deleted first (upsert via delete+insert).
// len(chunks) must equal len(embeddings).
func (s *Store) UpsertChunks(ctx context.Context, chunks []Chunk, embeddings [][]float32) error {
	if len(chunks) != len(embeddings) {
		return fmt.Errorf("chunk count (%d) != embedding count (%d)", len(chunks), len(embeddings))
	}
	if len(chunks) == 0 {
		return nil
	}

	for _, c := range chunks {
		filter := fmt.Sprintf("id = '%s'", c.ID)
		_ = s.table.Delete(ctx, filter)
	}

	record, err := s.chunksToRecord(chunks, embeddings)
	if err != nil {
		return fmt.Errorf("failed to build arrow record: %w", err)
	}
	defer record.Release()

	if err := s.table.AddRecords(ctx, []arrow.Record{record}, nil); err != nil {
		return fmt.Errorf("failed to add records: %w", err)
	}

	return nil
}

// DeleteByFilePath removes all chunks for the given repo-relative file path.
func (s *Store) DeleteByFilePath(ctx context.Context, filePath string) error {
	filter := fmt.Sprintf("file_path = '%s'", filePath)
	if err := s.table.Delete(ctx, filter); err != nil {
		return fmt.Errorf("failed to delete chunks for %s: %w", filePath, err)
	}
	return nil
}

// VectorSearch finds the top-K most similar chunks to the given query vector.
// Optional filters narrow results by language, chunk type, or file path prefix.
func (s *Store) VectorSearch(ctx context.Context, queryVec []float32, topK int, filters ...Filter) ([]SearchResult, error) {
	filterStr := buildFilterString(filters)

	var rows []map[string]any
	var err error

	if filterStr != "" {
		rows, err = s.table.VectorSearchWithFilter(ctx, "embedding", queryVec, topK, filterStr)
	} else {
		rows, err = s.table.VectorSearch(ctx, "embedding", queryVec, topK)
	}
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		chunk, score := rowToChunk(row)
		results = append(results, SearchResult{
			Chunk: chunk,
			Score: score,
		})
	}

	return results, nil
}

// ChunksByFilePath returns all stored chunks for a given file path.
// Used by the indexer for change detection (comparing content hashes).
func (s *Store) ChunksByFilePath(ctx context.Context, filePath string) ([]Chunk, error) {
	filter := fmt.Sprintf("file_path = '%s'", filePath)
	rows, err := s.table.SelectWithFilter(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to select chunks for %s: %w", filePath, err)
	}

	chunks := make([]Chunk, 0, len(rows))
	for _, row := range rows {
		chunk, _ := rowToChunk(row)
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// ChunksByName returns all stored chunks matching the given name field.
// Used by the searcher for one-hop dependency expansion via the call graph.
func (s *Store) ChunksByName(ctx context.Context, name string) ([]Chunk, error) {
	filter := fmt.Sprintf("name = '%s'", name)
	rows, err := s.table.SelectWithFilter(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to select chunks by name %s: %w", name, err)
	}

	chunks := make([]Chunk, 0, len(rows))
	for _, row := range rows {
		chunk, _ := rowToChunk(row)
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// Close releases the table and connection.
func (s *Store) Close() error {
	if s.table != nil {
		if err := s.table.Close(); err != nil {
			return err
		}
	}
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// chunksToRecord builds an Arrow record from chunks and their embeddings.
func (s *Store) chunksToRecord(chunks []Chunk, embeddings [][]float32) (arrow.Record, error) {
	schema, err := buildSchema()
	if err != nil {
		return nil, err
	}

	arrowSchema := schema.ToArrowSchema()
	builder := array.NewRecordBuilder(s.pool, arrowSchema)
	defer builder.Release()

	fID := builder.Field(0).(*array.StringBuilder)
	fProjectName := builder.Field(1).(*array.StringBuilder)
	fFilePath := builder.Field(2).(*array.StringBuilder)
	fLanguage := builder.Field(3).(*array.StringBuilder)
	fChunkType := builder.Field(4).(*array.StringBuilder)
	fName := builder.Field(5).(*array.StringBuilder)
	fSignature := builder.Field(6).(*array.StringBuilder)
	fBody := builder.Field(7).(*array.StringBuilder)
	fDescription := builder.Field(8).(*array.StringBuilder)
	fLineStart := builder.Field(9).(*array.Int32Builder)
	fLineEnd := builder.Field(10).(*array.Int32Builder)
	fContentHash := builder.Field(11).(*array.StringBuilder)
	fIndexedAt := builder.Field(12).(*array.TimestampBuilder)
	fCalls := builder.Field(13).(*array.StringBuilder)
	fCalledBy := builder.Field(14).(*array.StringBuilder)
	fTypesUsed := builder.Field(15).(*array.StringBuilder)
	fImplements := builder.Field(16).(*array.StringBuilder)
	fImports := builder.Field(17).(*array.StringBuilder)
	fEmbedding := builder.Field(18).(*array.FixedSizeListBuilder)
	fEmbValues := fEmbedding.ValueBuilder().(*array.Float32Builder)

	for i, c := range chunks {
		fID.Append(c.ID)
		fProjectName.Append(c.ProjectName)
		fFilePath.Append(c.FilePath)
		fLanguage.Append(c.Language)
		fChunkType.Append(c.ChunkType)
		fName.Append(c.Name)
		fSignature.Append(c.Signature)
		fBody.Append(c.Body)
		fDescription.Append(c.Description)
		fLineStart.Append(int32(c.LineStart))
		fLineEnd.Append(int32(c.LineEnd))
		fContentHash.Append(c.ContentHash)
		fIndexedAt.Append(arrow.Timestamp(c.IndexedAt.UnixMicro()))
		fCalls.Append(marshalJSON(c.Calls))
		fCalledBy.Append(marshalJSON(c.CalledBy))
		fTypesUsed.Append(marshalJSON(c.TypesUsed))
		fImplements.Append(marshalJSON(c.Implements))
		fImports.Append(marshalJSON(c.Imports))

		fEmbedding.Append(true)
		for _, v := range embeddings[i] {
			fEmbValues.Append(v)
		}
	}

	return builder.NewRecord(), nil
}

// buildFilterString constructs a SQL-like filter from the provided filters.
// Multiple filters are AND'd together. Empty filters are skipped.
func buildFilterString(filters []Filter) string {
	if len(filters) == 0 {
		return ""
	}

	var parts []string
	for _, f := range filters {
		if f.Language != "" {
			parts = append(parts, fmt.Sprintf("language = '%s'", f.Language))
		}
		if f.ChunkType != "" {
			parts = append(parts, fmt.Sprintf("chunk_type = '%s'", f.ChunkType))
		}
		if f.FilePath != "" {
			parts = append(parts, fmt.Sprintf("file_path LIKE '%s%%'", f.FilePath))
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, " AND ")
}

// rowToChunk converts a LanceDB result row (map) to a Chunk and extracts the
// distance score. LanceDB vector search returns a "_distance" key.
func rowToChunk(row map[string]any) (Chunk, float32) {
	c := Chunk{
		ID:          mapStr(row, "id"),
		ProjectName: mapStr(row, "project_name"),
		FilePath:    mapStr(row, "file_path"),
		Language:    mapStr(row, "language"),
		ChunkType:   mapStr(row, "chunk_type"),
		Name:        mapStr(row, "name"),
		Signature:   mapStr(row, "signature"),
		Body:        mapStr(row, "body"),
		Description: mapStr(row, "description"),
		LineStart:   mapInt(row, "line_start"),
		LineEnd:     mapInt(row, "line_end"),
		ContentHash: mapStr(row, "content_hash"),
	}

	if v, ok := row["indexed_at"]; ok {
		switch t := v.(type) {
		case time.Time:
			c.IndexedAt = t
		case int64:
			c.IndexedAt = time.UnixMicro(t)
		}
	}

	c.Calls = unmarshalFuncRefs(mapStr(row, "calls"))
	c.CalledBy = unmarshalFuncRefs(mapStr(row, "called_by"))
	c.TypesUsed = unmarshalStrings(mapStr(row, "types_used"))
	c.Implements = unmarshalStrings(mapStr(row, "implements_ifaces"))
	c.Imports = unmarshalStrings(mapStr(row, "imports"))

	var score float32
	if v, ok := row["_distance"]; ok {
		switch d := v.(type) {
		case float32:
			score = 1.0 - d
		case float64:
			score = 1.0 - float32(d)
		}
	}

	return c, score
}

// mapStr safely extracts a string from a map.
func mapStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// mapInt safely extracts an int from a map.
func mapInt(m map[string]any, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int32:
			return int(n)
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}

// DropAndRecreateTable drops the chunks table and recreates it with the current schema.
// Used for schema migrations when columns change.
func (s *Store) DropAndRecreateTable(ctx context.Context) error {
	if s.table != nil {
		_ = s.table.Close()
		s.table = nil
	}

	if err := s.conn.DropTable(ctx, tableName); err != nil {
		return fmt.Errorf("drop table %s: %w", tableName, err)
	}

	table, err := s.openOrCreateTable(ctx)
	if err != nil {
		return fmt.Errorf("recreate table: %w", err)
	}
	s.table = table
	return nil
}

// marshalJSON encodes a value as a JSON string. Returns "[]" for nil slices.
func marshalJSON(v any) string {
	if v == nil {
		return "[]"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// unmarshalFuncRefs decodes a JSON string into a slice of FuncRef.
func unmarshalFuncRefs(s string) []FuncRef {
	if s == "" || s == "[]" {
		return nil
	}
	var refs []FuncRef
	if err := json.Unmarshal([]byte(s), &refs); err != nil {
		return nil
	}
	return refs
}

// unmarshalStrings decodes a JSON string into a slice of strings.
func unmarshalStrings(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var strs []string
	if err := json.Unmarshal([]byte(s), &strs); err != nil {
		return nil
	}
	return strs
}

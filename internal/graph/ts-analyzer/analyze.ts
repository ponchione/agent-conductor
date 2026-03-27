import * as ts from "typescript";
import * as path from "path";

// NDJSON output types
interface SymbolOutput {
  type: "symbol";
  id: string;
  name: string;
  kind: string;
  package: string;
  file_path: string;
  line_start: number;
  line_end: number;
  signature: string;
  exported: boolean;
}

interface EdgeOutput {
  type: "edge";
  source_id: string;
  target_id: string;
  edge_type: string;
  confidence: number;
  source_line: number;
}

interface BoundaryOutput {
  type: "boundary";
  id: string;
  name: string;
  kind: string;
  package: string;
}

type OutputLine = SymbolOutput | EdgeOutput | BoundaryOutput;

function emit(line: OutputLine): void {
  process.stdout.write(JSON.stringify(line) + "\n");
}

function getModulePath(filePath: string, projectRoot: string): string {
  const rel = path.relative(projectRoot, filePath);
  const dir = path.dirname(rel);
  return dir === "." ? path.basename(rel, path.extname(rel)) : dir;
}

function symbolID(modulePath: string, kind: string, name: string): string {
  return `ts:${modulePath}:${kind}:${name}`;
}

function isExported(node: ts.Node): boolean {
  const modifiers = ts.canHaveModifiers(node)
    ? ts.getModifiers(node)
    : undefined;
  return (
    modifiers?.some((m) => m.kind === ts.SyntaxKind.ExportKeyword) ?? false
  );
}

function getSignature(node: ts.Node, sourceFile: ts.SourceFile): string {
  const start = node.getStart(sourceFile);
  const text = sourceFile.text;
  // Get up to the opening brace or end of line
  let end = text.indexOf("{", start);
  if (end === -1) end = text.indexOf("\n", start);
  if (end === -1) end = start + 200;
  return text.slice(start, end).trim();
}

function returnsJSX(
  node: ts.FunctionDeclaration | ts.ArrowFunction | ts.FunctionExpression
): boolean {
  let found = false;
  function check(n: ts.Node): void {
    if (found) return;
    if (
      ts.isJsxElement(n) ||
      ts.isJsxSelfClosingElement(n) ||
      ts.isJsxFragment(n)
    ) {
      found = true;
      return;
    }
    ts.forEachChild(n, check);
  }
  if (node.body) check(node.body);
  return found;
}

function extractSymbols(
  sourceFile: ts.SourceFile,
  projectRoot: string
): SymbolOutput[] {
  const symbols: SymbolOutput[] = [];
  const modulePath = getModulePath(sourceFile.fileName, projectRoot);

  function visit(node: ts.Node): void {
    const startLine =
      sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line +
      1;
    const endLine =
      sourceFile.getLineAndCharacterOfPosition(node.getEnd()).line + 1;

    if (ts.isFunctionDeclaration(node) && node.name) {
      const name = node.name.text;
      const kind = returnsJSX(node) ? "component" : "function";
      symbols.push({
        type: "symbol",
        id: symbolID(modulePath, kind, name),
        name,
        kind,
        package: modulePath,
        file_path: path.relative(projectRoot, sourceFile.fileName),
        line_start: startLine,
        line_end: endLine,
        signature: getSignature(node, sourceFile),
        exported: isExported(node),
      });
    }

    if (ts.isClassDeclaration(node) && node.name) {
      symbols.push({
        type: "symbol",
        id: symbolID(modulePath, "class", node.name.text),
        name: node.name.text,
        kind: "class",
        package: modulePath,
        file_path: path.relative(projectRoot, sourceFile.fileName),
        line_start: startLine,
        line_end: endLine,
        signature: getSignature(node, sourceFile),
        exported: isExported(node),
      });

      // Extract methods
      node.members.forEach((member) => {
        if (
          ts.isMethodDeclaration(member) &&
          member.name &&
          ts.isIdentifier(member.name)
        ) {
          const mStart =
            sourceFile.getLineAndCharacterOfPosition(
              member.getStart(sourceFile)
            ).line + 1;
          const mEnd =
            sourceFile.getLineAndCharacterOfPosition(member.getEnd()).line + 1;
          symbols.push({
            type: "symbol",
            id: symbolID(
              modulePath,
              "method",
              `${node.name!.text}.${member.name.text}`
            ),
            name: member.name.text,
            kind: "method",
            package: modulePath,
            file_path: path.relative(projectRoot, sourceFile.fileName),
            line_start: mStart,
            line_end: mEnd,
            signature: getSignature(member, sourceFile),
            exported: isExported(node),
          });
        }
      });
    }

    if (ts.isInterfaceDeclaration(node) && node.name) {
      symbols.push({
        type: "symbol",
        id: symbolID(modulePath, "interface", node.name.text),
        name: node.name.text,
        kind: "interface",
        package: modulePath,
        file_path: path.relative(projectRoot, sourceFile.fileName),
        line_start: startLine,
        line_end: endLine,
        signature: getSignature(node, sourceFile),
        exported: isExported(node),
      });
    }

    // Named arrow functions / function expressions at module scope
    if (ts.isVariableStatement(node)) {
      for (const decl of node.declarationList.declarations) {
        if (ts.isIdentifier(decl.name) && decl.initializer) {
          if (
            ts.isArrowFunction(decl.initializer) ||
            ts.isFunctionExpression(decl.initializer)
          ) {
            const kind = returnsJSX(decl.initializer) ? "component" : "function";
            symbols.push({
              type: "symbol",
              id: symbolID(modulePath, kind, decl.name.text),
              name: decl.name.text,
              kind,
              package: modulePath,
              file_path: path.relative(projectRoot, sourceFile.fileName),
              line_start: startLine,
              line_end: endLine,
              signature: getSignature(node, sourceFile),
              exported: isExported(node),
            });
          }
        }
      }
    }

    ts.forEachChild(node, visit);
  }

  visit(sourceFile);
  return symbols;
}

// Edge extraction
function extractEdges(
  sourceFile: ts.SourceFile,
  checker: ts.TypeChecker,
  projectRoot: string
): (EdgeOutput | BoundaryOutput)[] {
  const outputs: (EdgeOutput | BoundaryOutput)[] = [];
  const modulePath = getModulePath(sourceFile.fileName, projectRoot);
  const seenEdges = new Set<string>();

  function getContainingSymbolID(node: ts.Node): string | null {
    let current = node.parent;
    while (current) {
      if (ts.isFunctionDeclaration(current) && current.name) {
        const kind = returnsJSX(current) ? "component" : "function";
        return symbolID(modulePath, kind, current.name.text);
      }
      if (
        ts.isMethodDeclaration(current) &&
        current.name &&
        ts.isIdentifier(current.name)
      ) {
        const classDecl = current.parent;
        if (ts.isClassDeclaration(classDecl) && classDecl.name) {
          return symbolID(
            modulePath,
            "method",
            `${classDecl.name.text}.${current.name.text}`
          );
        }
      }
      if (ts.isArrowFunction(current) || ts.isFunctionExpression(current)) {
        const parent = current.parent;
        if (ts.isVariableDeclaration(parent) && ts.isIdentifier(parent.name)) {
          const kind = returnsJSX(current) ? "component" : "function";
          return symbolID(modulePath, kind, parent.name.text);
        }
      }
      current = current.parent;
    }
    return null;
  }

  function visit(node: ts.Node): void {
    // CALLS edges
    if (ts.isCallExpression(node)) {
      const sourceID = getContainingSymbolID(node);
      if (sourceID) {
        const symbol = checker.getSymbolAtLocation(node.expression);
        if (symbol) {
          const decl = symbol.valueDeclaration ?? symbol.declarations?.[0];
          if (decl) {
            const declFile = decl.getSourceFile();
            const isExternal = declFile.fileName.includes("node_modules");
            const sourceLine =
              sourceFile.getLineAndCharacterOfPosition(
                node.getStart(sourceFile)
              ).line + 1;

            if (isExternal) {
              // Boundary symbol
              const pkgName = getPackageName(declFile.fileName);
              const targetID = `ts:${pkgName}:function:${symbol.name}`;
              const key = `${sourceID}->${targetID}`;
              if (!seenEdges.has(key)) {
                seenEdges.add(key);
                outputs.push({
                  type: "edge",
                  source_id: sourceID,
                  target_id: targetID,
                  edge_type: "CALLS",
                  confidence: 1.0,
                  source_line: sourceLine,
                });
                outputs.push({
                  type: "boundary",
                  id: targetID,
                  name: symbol.name,
                  kind: "function",
                  package: pkgName,
                });
              }
            } else {
              const targetModulePath = getModulePath(
                declFile.fileName,
                projectRoot
              );
              const targetKind = ts.isClassDeclaration(decl)
                ? "class"
                : "function";
              const targetID = symbolID(targetModulePath, targetKind, symbol.name);
              const key = `${sourceID}->${targetID}`;
              if (!seenEdges.has(key)) {
                seenEdges.add(key);
                outputs.push({
                  type: "edge",
                  source_id: sourceID,
                  target_id: targetID,
                  edge_type: "CALLS",
                  confidence: 1.0,
                  source_line: sourceLine,
                });
              }
            }
          }
        }
      }
    }

    // IMPORTS edges
    if (
      ts.isImportDeclaration(node) &&
      node.moduleSpecifier &&
      ts.isStringLiteral(node.moduleSpecifier)
    ) {
      const importPath = node.moduleSpecifier.text;
      const sourceID = symbolID(
        modulePath,
        "module",
        path.basename(sourceFile.fileName, path.extname(sourceFile.fileName))
      );
      const sourceLine =
        sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile))
          .line + 1;

      const isRelative = importPath.startsWith(".");
      if (isRelative) {
        const resolvedModule = path.resolve(
          path.dirname(sourceFile.fileName),
          importPath
        );
        const targetModulePath = getModulePath(resolvedModule, projectRoot);
        const targetID = `ts:${targetModulePath}:module:${path.basename(importPath)}`;
        outputs.push({
          type: "edge",
          source_id: sourceID,
          target_id: targetID,
          edge_type: "IMPORTS",
          confidence: 1.0,
          source_line: sourceLine,
        });
      } else {
        const targetID = `ts:${importPath}:module:${importPath}`;
        outputs.push({
          type: "edge",
          source_id: sourceID,
          target_id: targetID,
          edge_type: "IMPORTS",
          confidence: 1.0,
          source_line: sourceLine,
        });
        outputs.push({
          type: "boundary",
          id: targetID,
          name: importPath,
          kind: "module",
          package: importPath,
        });
      }
    }

    // EXTENDS / IMPLEMENTS edges (heritage clauses)
    if (
      ts.isClassDeclaration(node) &&
      node.name &&
      node.heritageClauses
    ) {
      for (const clause of node.heritageClauses) {
        const edgeType =
          clause.token === ts.SyntaxKind.ExtendsKeyword
            ? "EXTENDS"
            : "IMPLEMENTS";
        for (const type of clause.types) {
          const symbol = checker.getSymbolAtLocation(type.expression);
          if (symbol) {
            const decl = symbol.valueDeclaration ?? symbol.declarations?.[0];
            if (decl) {
              const declFile = decl.getSourceFile();
              const targetModulePath = getModulePath(
                declFile.fileName,
                projectRoot
              );
              const targetKind = ts.isInterfaceDeclaration(decl)
                ? "interface"
                : "class";
              const targetID = symbolID(
                targetModulePath,
                targetKind,
                symbol.name
              );
              const sourceLine =
                sourceFile.getLineAndCharacterOfPosition(
                  type.getStart(sourceFile)
                ).line + 1;
              outputs.push({
                type: "edge",
                source_id: symbolID(modulePath, "class", node.name.text),
                target_id: targetID,
                edge_type: edgeType,
                confidence: 1.0,
                source_line: sourceLine,
              });
            }
          }
        }
      }
    }

    // INSTANTIATES edges (new expressions)
    if (ts.isNewExpression(node)) {
      const sourceID = getContainingSymbolID(node);
      if (sourceID) {
        const symbol = checker.getSymbolAtLocation(node.expression);
        if (symbol) {
          const decl = symbol.valueDeclaration ?? symbol.declarations?.[0];
          if (decl && !decl.getSourceFile().fileName.includes("node_modules")) {
            const declFile = decl.getSourceFile();
            const targetModulePath = getModulePath(
              declFile.fileName,
              projectRoot
            );
            const targetID = symbolID(targetModulePath, "class", symbol.name);
            const sourceLine =
              sourceFile.getLineAndCharacterOfPosition(
                node.getStart(sourceFile)
              ).line + 1;
            const key = `${sourceID}->${targetID}:INSTANTIATES`;
            if (!seenEdges.has(key)) {
              seenEdges.add(key);
              outputs.push({
                type: "edge",
                source_id: sourceID,
                target_id: targetID,
                edge_type: "INSTANTIATES",
                confidence: 1.0,
                source_line: sourceLine,
              });
            }
          }
        }
      }
    }

    ts.forEachChild(node, visit);
  }

  visit(sourceFile);
  return outputs;
}

function getPackageName(filePath: string): string {
  const nodeModulesIdx = filePath.lastIndexOf("node_modules/");
  if (nodeModulesIdx === -1) return "unknown";
  const afterNodeModules = filePath.slice(
    nodeModulesIdx + "node_modules/".length
  );
  if (afterNodeModules.startsWith("@")) {
    const parts = afterNodeModules.split("/");
    return parts.slice(0, 2).join("/");
  }
  return afterNodeModules.split("/")[0];
}

// Main entry point
function main(): void {
  const args = process.argv.slice(2);
  if (args.length < 1) {
    process.stderr.write("Usage: analyze.ts <projectRoot> [tsconfigPath]\n");
    process.exit(1);
  }

  const projectRoot = path.resolve(args[0]);
  const tsconfigPath = args[1]
    ? path.resolve(args[1])
    : path.join(projectRoot, "tsconfig.json");

  const configFile = ts.readConfigFile(tsconfigPath, ts.sys.readFile);
  if (configFile.error) {
    process.stderr.write(
      `Error reading tsconfig: ${ts.flattenDiagnosticMessageText(configFile.error.messageText, "\n")}\n`
    );
    process.exit(1);
  }

  const parsedConfig = ts.parseJsonConfigFileContent(
    configFile.config,
    ts.sys,
    projectRoot
  );

  const program = ts.createProgram(parsedConfig.fileNames, parsedConfig.options);
  const checker = program.getTypeChecker();

  for (const sourceFile of program.getSourceFiles()) {
    if (sourceFile.isDeclarationFile) continue;
    if (sourceFile.fileName.includes("node_modules")) continue;

    const symbols = extractSymbols(sourceFile, projectRoot);
    symbols.forEach(emit);

    const edges = extractEdges(sourceFile, checker, projectRoot);
    edges.forEach(emit);
  }
}

main();

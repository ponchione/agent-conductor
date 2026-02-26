#!/bin/bash

# Configuration
ENDPOINT="http://localhost:8080/v1/chat/completions"
MODEL="DeepSeek-R1-Distill-Qwen-32B"

# Test Data
SYSTEM_PROMPT="You are a code verifier. Respond only in JSON. Use <think> tags for reasoning."
USER_MESSAGE="Verify this Go code: 'func Add(a, b int) int { return a + b }'. Context: Must use a mutex for all additions. Criteria: 1. Uses sync.Mutex."

echo "--- SENDING REQUEST TO $MODEL ---"

# Send request and capture response
RESPONSE=$(
curl -s -X POST "$ENDPOINT" \
  -H "Content-Type: application/json" \
  -d @- <<EOF
{
  "model": "$MODEL",
  "messages": [
    {"role": "system", "content": "$SYSTEM_PROMPT"},
    {"role": "user", "content": "$USER_MESSAGE"}
  ],
  "temperature": 0.0
}
EOF
)

# Check if curl succeeded
if [ $? -ne 0 ]; then
    echo "Error: Could not reach the LLM server."
    exit 1
fi

# Extract the content field using jq
RAW_CONTENT=$(echo "$RESPONSE" | jq -r '.choices[0].message.content')

echo -e "\n--- RAW LLM OUTPUT ---"
echo "$RAW_CONTENT"

echo -e "\n--- ATTEMPTING TO EXTRACT JSON ---"
# Fixed sed: 1. Remove <think> blocks. 2. Remove code fences.
CLEANED_JSON=$(echo "$RAW_CONTENT" | sed 's/<think>.*<\/think>//g' | sed 's/```json//g' | sed 's/```//g')

# Try to parse; if it's incomplete, jq will tell us why
echo "$CLEANED_JSON" | jq .
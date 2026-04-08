/**
 * curator_agent.ts
 *
 * Reads pending inbox entries from stdin (JSON array), then runs a
 * Claude Agent SDK session connected to the memory-server MCP endpoint.
 * Claude autonomously searches existing memories/KB pages and decides
 * whether to create new entries, update existing ones, or skip — using
 * the MCP tools provided by the server.
 *
 * Environment variables (all set by the Go curator service):
 *   ANTHROPIC_API_KEY         – Anthropic API key (required)
 *   MEMORY_SERVER_MCP_URL     – memory-server MCP endpoint
 *                               (default: http://localhost:8080/mcp)
 *   MEMORY_SERVER_TOKEN       – Bearer token for the memory-server API
 *                               (optional; omit when AUTH_ENABLED=false)
 *   CURATOR_USER_ID           – user_id to scope memory/KB operations
 *                               (default: "default")
 *
 * stdin:  JSON array of inbox.Entry objects
 * stdout: JSON array of processed inbox_ids  e.g. ["id1","id2"]
 * stderr: diagnostic / error messages
 * exit 0: success (stdout contains processed ids)
 * exit 1: fatal error
 */

import { query, type SDKMessage } from "@anthropic-ai/claude-agent-sdk";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface InboxEntry {
  inbox_id: string;
  user_id: string;
  content: string;
  source?: string;
  tags?: string[];
  created_at?: string;
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const MCP_URL =
  process.env.MEMORY_SERVER_MCP_URL ?? "http://localhost:8080/mcp";
const TOKEN = process.env.MEMORY_SERVER_TOKEN ?? "";
const USER_ID = process.env.CURATOR_USER_ID ?? "default";

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const stdinText = await Bun.stdin.text();

  let entries: InboxEntry[];
  try {
    entries = JSON.parse(stdinText);
  } catch (err) {
    process.stderr.write(`Failed to parse stdin as JSON: ${err}\n`);
    process.exit(1);
  }

  if (!Array.isArray(entries) || entries.length === 0) {
    process.stdout.write("[]\n");
    return;
  }

  const processedIds: string[] = [];

  // Process entries in batches to keep context manageable.
  // Each batch gets its own agent session so tool call history stays short.
  const BATCH_SIZE = 10;
  for (let i = 0; i < entries.length; i += BATCH_SIZE) {
    const batch = entries.slice(i, i + BATCH_SIZE);
    const ids = await processBatch(batch);
    processedIds.push(...ids);
  }

  process.stdout.write(JSON.stringify(processedIds) + "\n");
}

// ---------------------------------------------------------------------------
// Process a batch of entries with one agent session
// ---------------------------------------------------------------------------

async function processBatch(entries: InboxEntry[]): Promise<string[]> {
  const mcpHeaders: Record<string, string> = {};
  if (TOKEN) {
    mcpHeaders["Authorization"] = `Bearer ${TOKEN}`;
  }

  const prompt = buildPrompt(entries, USER_ID);

  process.stderr.write(
    `[curator] starting agent session for ${entries.length} entries via ${MCP_URL}\n`
  );

  let finalResult = "";
  let errorOccurred = false;

  try {
    for await (const message of query({
      prompt,
      options: {
        // No local file-system tools needed — all operations go via MCP.
        allowedTools: [],
        permissionMode: "bypassPermissions",
        allowDangerouslySkipPermissions: true,
        maxTurns: 40,
        mcpServers: {
          "memory-server": {
            type: "sse",
            url: MCP_URL,
            ...(Object.keys(mcpHeaders).length > 0 && {
              requestInit: { headers: mcpHeaders },
            }),
          },
        },
      },
    })) {
      logMessage(message);

      if (message.type === "result") {
        if (message.subtype === "success") {
          finalResult = message.result ?? "";
        } else {
          process.stderr.write(
            `[curator] agent finished with subtype=${message.subtype}\n`
          );
          errorOccurred = true;
        }
      }
    }
  } catch (err) {
    process.stderr.write(`[curator] claude-agent-sdk error: ${err}\n`);
    process.exit(1);
  }

  if (errorOccurred) {
    // Return no processed IDs for this batch — Go will retry next run.
    return [];
  }

  // Extract the list of processed inbox_ids from the agent's final reply.
  return extractProcessedIds(finalResult, entries);
}

// ---------------------------------------------------------------------------
// Prompt
// ---------------------------------------------------------------------------

function buildPrompt(entries: InboxEntry[], userID: string): string {
  return `You are a memory curator assistant for user "${userID}".

Your task is to process the following inbox entries and organise them into the
memory system using the MCP tools available to you.

## Inbox entries to process

${JSON.stringify(entries, null, 2)}

## Instructions

For each entry, follow these steps:

1. **Search first** — use \`search_memories\` and \`search_kb\` to find existing
   content related to the entry.

2. **Decide the action**:
   - If closely related memory exists → **update it** with the new information
     using \`update_memory\`.
   - If closely related KB page exists → **update it** using \`update_kb_page\`.
   - If the content is factual, experiential, or a short note → **create a new
     memory** using \`add_memory\`.
   - If the content is structured knowledge (how-to, reference, guide) →
     **create a KB page** using \`create_kb_page\`.
   - If the content is duplicate, spam, or too vague → **skip it**.
   - An entry can result in both a memory and a KB page if appropriate.

3. Always set \`user_id\` to "${userID}" in every tool call.

4. After processing all entries, reply with a JSON array of the inbox_ids you
   successfully handled (created or updated something, or intentionally skipped):

\`\`\`json
["inbox_id_1", "inbox_id_2"]
\`\`\`

Only include ids you actually processed. If a tool call failed and you could not
handle the entry, omit its id.`;
}

// ---------------------------------------------------------------------------
// Extract processed ids from the agent's final text
// ---------------------------------------------------------------------------

function extractProcessedIds(
  text: string,
  entries: InboxEntry[]
): string[] {
  // Try to find a JSON array in the response.
  const fenceMatch = text.match(/```(?:json)?\s*(\[[\s\S]*?\])\s*```/);
  const arrayMatch = fenceMatch
    ? fenceMatch[1]
    : text.match(/\[[\s\S]*?\]/)?.[0];

  if (arrayMatch) {
    try {
      const ids = JSON.parse(arrayMatch);
      if (Array.isArray(ids) && ids.every((id) => typeof id === "string")) {
        return ids;
      }
    } catch {
      // fall through
    }
  }

  // Fallback: if the agent mentioned processing all entries, return all ids.
  const allIds = entries.map((e) => e.inbox_id);
  const lowerText = text.toLowerCase();
  if (
    lowerText.includes("all entries") ||
    lowerText.includes("all inbox") ||
    lowerText.includes("processed all")
  ) {
    process.stderr.write(
      "[curator] could not parse id list; assuming all entries processed\n"
    );
    return allIds;
  }

  // Last resort: return ids that appear in the text.
  return allIds.filter((id) => text.includes(id));
}

// ---------------------------------------------------------------------------
// Logging
// ---------------------------------------------------------------------------

function logMessage(message: SDKMessage): void {
  switch (message.type) {
    case "assistant": {
      const content = (message as any).message?.content ?? [];
      for (const block of content) {
        if (block.type === "text") {
          process.stderr.write(`[claude] ${block.text.slice(0, 200)}\n`);
        } else if (block.type === "tool_use") {
          process.stderr.write(
            `[tool→] ${block.name}(${JSON.stringify(block.input).slice(0, 120)})\n`
          );
        }
      }
      break;
    }
    case "result":
      process.stderr.write(
        `[curator] session done — subtype=${message.subtype} cost=$${(message as any).total_cost_usd?.toFixed(4) ?? "?"}\n`
      );
      break;
    default:
      break;
  }
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

main().catch((err) => {
  process.stderr.write(`[curator] unhandled error: ${err}\n`);
  process.exit(1);
});

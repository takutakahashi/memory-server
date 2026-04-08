/**
 * curator_agent.ts
 *
 * Reads pending inbox entries from stdin (JSON array), runs a Claude Agent SDK
 * session to classify each entry, and writes the resulting action list to stdout
 * as a JSON array.
 *
 * Environment:
 *   ANTHROPIC_API_KEY  – Anthropic API key (required). Set by the Go curator
 *                        service, overriding the process-level env when a
 *                        per-request key is supplied via X-Anthropic-Key header.
 *
 * stdin:  JSON array of inbox.Entry objects
 * stdout: JSON array of CuratorAction objects
 * stderr: diagnostic / error messages
 */

import { query } from "@anthropic-ai/claude-agent-sdk";

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

interface MemoryPayload {
  content: string;
  tags: string[];
  scope: "private" | "public";
}

interface KBPagePayload {
  title: string;
  slug: string;
  content: string;
  summary: string;
  category: string;
  tags: string[];
  scope: "private" | "public";
}

interface CuratorAction {
  inbox_id: string;
  action: "memory" | "kb" | "both" | "skip";
  memory?: MemoryPayload;
  kb_page?: KBPagePayload;
  skip_reason?: string;
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  // Read all of stdin
  const stdinText = await Bun.stdin.text();

  let entries: InboxEntry[];
  try {
    entries = JSON.parse(stdinText);
  } catch (err) {
    process.stderr.write(`Failed to parse stdin as JSON: ${err}\n`);
    process.exit(1);
  }

  if (!Array.isArray(entries) || entries.length === 0) {
    // Nothing to do
    process.stdout.write("[]\n");
    return;
  }

  const prompt = buildPrompt(entries);

  let finalText = "";
  try {
    for await (const message of query({
      prompt,
      options: {
        // No file-system tools needed — Claude only needs to reason and output JSON.
        allowedTools: [],
        permissionMode: "dontAsk",
        maxTurns: 3,
      },
    })) {
      if (message.type === "result" && message.subtype === "success") {
        finalText = message.result ?? "";
      } else if (message.type === "result" && message.subtype !== "success") {
        process.stderr.write(
          `Claude agent finished with non-success subtype: ${message.subtype}\n`
        );
        process.exit(1);
      }
    }
  } catch (err) {
    process.stderr.write(`claude-agent-sdk error: ${err}\n`);
    process.exit(1);
  }

  // Claude might wrap the JSON in a markdown fence — extract the array.
  const jsonText = extractJSON(finalText);
  if (!jsonText) {
    process.stderr.write(
      `No JSON array found in Claude response:\n${finalText}\n`
    );
    process.exit(1);
  }

  // Validate that the output is parseable before forwarding.
  try {
    JSON.parse(jsonText);
  } catch (err) {
    process.stderr.write(
      `Claude response JSON is invalid (${err}):\n${jsonText}\n`
    );
    process.exit(1);
  }

  process.stdout.write(jsonText + "\n");
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function buildPrompt(entries: InboxEntry[]): string {
  return `You are a memory curator. Analyse each inbox entry and decide what to create.

Rules:
- "memory"  → a fact, experience, or short-lived information worth remembering
- "kb"      → structured, reusable knowledge, how-to guides, or reference material
- "both"    → create both a memory AND a KB page
- "skip"    → duplicate, spam, or not worth storing

Inbox entries:
${JSON.stringify(entries, null, 2)}

Reply with ONLY a valid JSON array — no markdown fences, no explanation text.
One element per entry, strictly following this schema:

[
  {
    "inbox_id": "<entry inbox_id>",
    "action": "memory" | "kb" | "both" | "skip",
    "memory": {
      "content": "<concise memory text>",
      "tags": ["tag1", "tag2"],
      "scope": "private"
    },
    "kb_page": {
      "title": "<page title>",
      "slug": "<url-safe-slug>",
      "content": "<full markdown content>",
      "summary": "<one-sentence summary>",
      "category": "<category>",
      "tags": ["tag1", "tag2"],
      "scope": "private"
    },
    "skip_reason": "<reason if action is skip>"
  }
]

Include "memory" only when action is "memory" or "both".
Include "kb_page" only when action is "kb" or "both".
Include "skip_reason" only when action is "skip".`;
}

function extractJSON(text: string): string | null {
  // Strip markdown code fences if present
  const fenceMatch = text.match(/```(?:json)?\s*([\s\S]*?)```/);
  if (fenceMatch) {
    return fenceMatch[1].trim();
  }

  // Find the outermost JSON array
  const arrayMatch = text.match(/\[[\s\S]*\]/);
  if (arrayMatch) {
    return arrayMatch[0];
  }

  return null;
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

main().catch((err) => {
  process.stderr.write(`Unhandled error: ${err}\n`);
  process.exit(1);
});

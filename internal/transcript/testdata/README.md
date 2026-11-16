# Transcript test fixtures

`sample.jsonl` holds redacted lines derived from a real Claude Code transcript
(2026-10-21). Every structural field is preserved; all human-readable content
(`text`, `thinking`, tool input, tool results) is replaced with `"REDACTED"`.
Derived data only, per the project's privacy constraint.

One line per verified format fact:

1. `user` with string `content` and `isMeta:false` - a typed human prompt (no content blocks).
2. `assistant` whose `message.content` holds a `tool_use` block `{type,name,id}` - a tool call; `id` is `toolu_...`.
3. `user` whose `message.content` holds the matching `tool_result` block `{type,tool_use_id}` - the call's result; `tool_use_id` equals the call's `id`.
4. `assistant` whose `message.content` holds a `thinking` block - confirms thinking blocks are present and carry no top-level duration.
5. `api-request` - a non user/assistant line the capture core ignores; confirms the decoder tolerates other types.

Note: `cwd` is a Windows backslash path and `timestamp` is RFC3339 with a
millisecond fraction and `Z`, matching the host these transcripts come from.

`codex.jsonl` is an invented Codex rollout: every line's shape copies a real
rollout (`~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl`, 2026-10-29), every
text is made up. It has the session_meta, a turn from task_started to
task_complete, a developer message, two injected user messages (AGENTS.md
plus environment_context, and an open-pages tag), the typed prompt, assistant
messages, reasoning, a custom and a function tool call with their outputs, a
token_count with rate limits, and an item_completed after the turn.

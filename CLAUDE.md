# CLAUDE.md — Accumen Orchestration (Local Windows Setup)

This doc tells Claude Code exactly how to **write files locally to `C:\Dev\accumulate-accumen`**.  
You will commit between prompts yourself.

---

## Prereqs

- Windows PowerShell (recommended).
- Your local directories:
  - Repo target (write location): `C:\Dev\accumulate-accumen`
  - Accumulate official repo (read-only reference): `C:\Accumulate_Stuff\accumulate`
  - Accumulate VDK (read-only reference): `C:\Accumulate_Stuff\accumulate\vdk`
  - Optional TS client (read-only reference): `C:\Accumulate_Stuff\accumulate-javascript-client`

> We are **not** wiring ledger support nor incomplete SDKs. For SDKs, use the **TypeScript** and **Go** SDKs already inside the official Accumulate repo.

---

## How to run Claude Code (local)

Run the Claude Code CLI with these arguments **every time** you paste a prompt from the Sequence section:

```powershell
claude --ide `
  --allowed-tools "Bash(dart*|pwsh*|powershell*|cmake*|ninja*|git*|where*|cmd*|dumpbin*), Read, Write, Edit" `
  --add-dir "C:\Dev\accumulate-accumen" `
  --add-dir "C:\Accumulate_Stuff\accumulate" `
  --add-dir "C:\Accumulate_Stuff\accumulate\vdk" `
  --add-dir "C:\Accumulate_Stuff\accumulate-javascript-client" `
  --permission-mode acceptEdits
The first --add-dir is the write target (your empty repo).
The others are read-only references so Claude can inspect Accumulate + VDK code.

Prompting pattern (important)
Always output files using fenced code blocks with the file path as the code fence language, e.g.:

r
Copy code
```path\to\file.ext
// file content here
Copy code
Do not run commands; just write/edit files.

Prefer imports from gitlab.com/accumulatenetwork/accumulate/....

Keep code compilable stubs where final logic isn’t implemented yet.

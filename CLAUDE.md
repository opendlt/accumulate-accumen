# CLAUDE.md — Accumen Orchestration (Local Windows Setup)

This doc tells Claude Code **exactly how to write files locally** to `C:\Dev\accumulate-accumen`.  
You will commit between prompts yourself.

---

## Prereqs

- Windows PowerShell (recommended).
- Your local directories:
  - **Write target** (repo root): `C:\Dev\accumulate-accumen`
  - Accumulate official repo (read-only reference): `C:\Accumulate_Stuff\accumulate`
  - Accumulate VDK (read-only reference): `C:\Accumulate_Stuff\accumulate\vdk`
  - Optional TS client (read-only reference): `C:\Accumulate_Stuff\accumulate-javascript-client`

> We are **not** wiring Ledger support nor incomplete SDKs.  
> For SDKs, use the **TypeScript** and **Go** SDKs in the official Accumulate repo.

---

## How to run Claude Code (local)

Run this **every time** you paste one of the prompts from the *Sequence* section:

```powershell
claude --ide `
  --allowed-tools "Bash(dart*|pwsh*|powershell*|cmake*|ninja*|git*|where*|cmd*|dumpbin*), Read, Write, Edit" `
  --add-dir "C:\Dev\accumulate-accumen" `
  --add-dir "C:\Accumulate_Stuff\accumulate" `
  --add-dir "C:\Accumulate_Stuff\accumulate\vdk" `
  --add-dir "C:\Accumulate_Stuff\accumulate-javascript-client" `
  --permission-mode acceptEdits
````

### Important

* The first `--add-dir` is the **write target**. All file paths in prompts are **relative to this directory**.
* Other `--add-dir`s are for **reading/reference only**.
* **You commit between prompts.**

---

## Filesystem Contract (read this to Claude in each prompt)

* You **MUST** use the **Write** tool to create or overwrite files under the first `--add-dir` (`C:\Dev\accumulate-accumen`).
* Merely printing fenced code blocks is not sufficient.
* All file paths are **relative to the first `--add-dir`**. Do **not** use absolute drive paths.
* Use **forward slashes** in paths (Windows accepts them in repos).
* One fenced block per file: use code fences with the **file path as the language tag**:

````text
```path/to/file.ext
// file content
````

```
- After generating the fenced blocks, **apply them to disk via the Write tool**.  
- If Write fails, retry or report the exact error.

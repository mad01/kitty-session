# Repo finder

`ks repo` scans your configured directories for git repositories and returns them interactively or in machine-readable formats. It's the same discovery layer that powers the TUI repo picker.

## What it scans

Everything under `dirs` in `~/.config/ks/config.yaml`. Missing directories are silently skipped; `ks` doesn't fail if one root is unavailable (for example, an unmounted external drive).

## How discovery works

The walker (`internal/repo/finder`) does a concurrent breadth-first search with 32 workers. For each directory it reads the entries once:

- If `.git` is present, the directory is recorded as a repo and its subtree is **not** descended further.
- Otherwise every non-dotfile subdirectory is queued for scanning.

No git subprocess runs at any point. `ks` parses `.git/config` directly by scanning for the `[remote "origin"]` section and reading its `url = ` line.

The result is deduplicated by absolute path before being returned.

## Name extraction

Names come from parsing the `origin` URL:

| Input | Extracted name |
|---|---|
| `git@github.com:mad01/kitty-session.git` | `mad01/kitty-session` |
| `https://github.com/mad01/kitty-session.git` | `mad01/kitty-session` |
| `git@git.corp.internal:team/service` | `team/service` |
| `https://gitlab.com/group/subgroup/project.git` | `subgroup/project` |

For deeper HTTPS paths (GitLab subgroups), only the last two path components are kept — this matches how the TUI displays repos.

If there is no `origin` remote, or the file can't be read, `ks` falls back to `<parent-dir>/<dir>`. That fallback loses host information (`Remote` and `Host` fields come back empty in JSON/TOON output).

## Host extraction

`Host` is the DNS name from the remote URL. It's handy when repos from multiple git hosts coexist under the same root.

| Input | Host |
|---|---|
| `git@github.com:org/repo.git` | `github.com` |
| `deploy@git.corp.com:infra/admission.git` | `git.corp.com` |
| `https://git.example.com/team/service.git` | `git.example.com` |

## Output formats

### Interactive (default)

```bash
ks repo
```

Opens a fuzzy finder (`ktr0731/go-fuzzyfinder`) with `name @ path` labels. Picking an entry prints the absolute path to stdout. Pressing `esc` exits silently with status `0` and no output — this is what the shell function below relies on.

### `--list`

```bash
ks repo --list
# mad01/kitty-session    /Users/you/code/src/github.com/mad01/kitty-session
# mad01/dotfiles         /Users/you/code/src/github.com/mad01/dotfiles
```

Tab-separated. Columns are `name`, `path`. Suitable for piping into `awk`, `cut`, or `fzf`.

### `--json`

```bash
ks repo --json
```

```json
[
  {
    "name": "mad01/kitty-session",
    "path": "/Users/you/code/src/github.com/mad01/kitty-session",
    "remote": "git@github.com:mad01/kitty-session.git",
    "host": "github.com"
  }
]
```

`remote` and `host` are omitted when empty (the fallback-name case).

### `--toon`

```bash
ks repo --toon
```

```
repos[1]{name,path,remote,host}:
  mad01/kitty-session,/Users/you/code/src/github.com/mad01/kitty-session,git@github.com:mad01/kitty-session.git,github.com
```

[TOON](https://github.com/alpkeskin/gotoon) is a token-efficient encoding designed for feeding structured data to LLMs. For typical repo lists it's 30–60% smaller than the equivalent JSON.

## Shell function

Drop this into your shell config to jump to a repo with one command:

```bash
repo() { local d=$(ks repo); [[ -n "$d" ]] && cd "$d"; }
```

Typing `repo` opens the fuzzy finder. Picking a repo `cd`s into it; pressing `esc` leaves you where you were.

## Empty result

If `dirs` is empty, unset, or contains only missing paths, `ks repo` exits with:

```
no git repositories found
```

and status `1`. The flag variants behave the same way — they do not print an empty JSON array.

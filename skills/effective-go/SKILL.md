---
name: effective-go
description: Compact rules and idioms from Effective Go for writing clear idiomatic Go code. Use when writing, reviewing, refactoring, or explaining Go — formatting, naming, control flow, functions, data, concurrency, errors, interfaces, packages.
---

# Effective Go — Compact Rules

Apply these rules when writing or reviewing Go. Prefer standard library examples and `gofmt`. Document is from 2009; core language idioms still hold (generics/modules are outside its scope).

## Formatting

- Always run `gofmt` / `go fmt`. No exceptions.
- Tabs for indentation. Spaces only if forced.
- No line-length limit; wrap and indent with an extra tab if needed.
- Fewer parentheses than C/Java: no parens around `if`/`for`/`switch` conditions.
- Opening brace of control structures stays on the same line (semicolon insertion rule).

## Commentary

- Prefer `//` line comments. Use `/* */` mainly for package docs or disabling large blocks.
- Doc comments sit immediately before the declaration they document (no blank line).
- Comment the *why*, not the *what*.

## Names

- **Packages**: short, lowercase, single-word, no underscores or MixedCaps. Base name of the directory. Avoid `util`/`common`.
- Exported names start with uppercase; unexported with lowercase.
- **Getters**: for field `owner` the method is `Owner()`, not `GetOwner()`. Setter is `SetOwner()`.
- **Interfaces**: one-method interfaces often named `Method` + `er` (`Reader`, `Writer`, `Stringer`). Honor canonical names/signatures (`Read`, `Write`, `String`, `Close`…).
- Multi-word names use MixedCaps / mixedCaps, never underscores.
- Short names for short scopes (`i`, `r`, `err`, `ctx`). Longer names for longer-lived or exported identifiers.
- Avoid stuttering with the package name (`bufio.Reader` not `bufio.BufReader`).

## Semicolons

- Almost never written; lexer inserts them. Consequence: opening `{` of `if`/`for`/`switch`/`select` must be on the same line.

## Control structures

- No `do`/`while`; only `for` (three forms: C-style, while-style, infinite).
- `if`/`switch` may have an init statement.
- Prefer early return; omit `else` after a terminating statement (`return`/`break`/`continue`/`goto`).
- Happy path stays at the lowest indentation.
- `switch` has no automatic fall-through; cases can be comma-separated lists. Expressionless `switch` acts as `if-else` chain.
- Type switch: `switch v := x.(type)`.
- `break`/`continue` accept optional labels.

## Functions

- Multiple return values are idiomatic (especially `(T, error)`).
- Named result parameters are optional; useful for documentation and bare `return`.
- `defer` for cleanup (close, unlock). Args evaluated at `defer` time; runs LIFO on function exit.
- Prefer small functions; composition over large ones.

## Data

- `new(T)` → pointer to zero value. `make` only for slices, maps, channels.
- Prefer slices over arrays for most sequences.
- Append is the idiomatic way to grow slices.
- Maps: use comma-ok idiom `v, ok := m[k]` to distinguish missing key from zero value.
- Blank identifier `_` discards values.

## Methods & interfaces

- Methods can be on any named type (including non-structs).
- Interfaces are satisfied implicitly. Define interfaces where they are *used*, not where they are implemented.
- Keep interfaces small (ideally 1–3 methods).
- Embedding for composition, not inheritance.

## Concurrency

- Prefer channels for communication; share memory by communicating.
- Do not start a goroutine without a clear way it will finish (WaitGroup, done channel, errgroup, context cancellation).
- `select` for multi-way channel operations.
- Avoid storing `context.Context` in structs; pass it as first parameter and propagate downward.

## Errors

- Check errors immediately; never ignore them.
- Return errors, do not panic for expected failures.
- Wrap with context: `fmt.Errorf("…: %w", err)`.
- Sentinel errors or custom types when callers need to inspect.

## Packages & program structure

- One package per directory. Package name = directory base name.
- Init functions run before `main`; keep them simple or avoid side effects.
- Use `go test` examples and the standard library sources as living documentation.

## Quick checklist when writing Go

1. `gofmt` everything.
2. Short package names; MixedCaps identifiers; no `Get` prefix on getters.
3. Early returns; happy path left-aligned.
4. Check every error.
5. Small interfaces defined at the consumer.
6. `defer` for cleanup.
7. Channels + select over shared mutable state when possible.
8. Comment why, not what.

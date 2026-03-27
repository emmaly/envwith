# envwith

Run a command with environment variables loaded from a `.env` file.

Unlike `source .env`, envwith does **not** modify your current shell. It reads the file, merges the variables onto the current environment, and execs the child process directly.

## Install

```sh
go install github.com/emmaly/envwith@latest
```

Or build from source:

```sh
go build -o envwith .
```

## Usage

```sh
envwith [options] [--] command [args...]
```

| Flag | Description |
|------|-------------|
| `-f FILE` | Env file to load (default: `.env`) |
| `--` | Optional separator before the command |

### Examples

```sh
# Run a server with vars from .env
envwith -- ./myserver --port 8080

# Use a specific env file
envwith -f production.env -- ./deploy.sh

# Quick inspection
envwith -- env | grep DATABASE
```

## .env file format

```sh
# Comments and blank lines are ignored
DATABASE_URL=postgres://localhost/mydb

# Quotes
GREETING="hello world"      # double-quoted: substitution + escapes (\n \t \\ \" \$)
LITERAL='no $substitution'  # single-quoted: literal value

# Variable substitution
BASE=/opt/app
CONFIG=${BASE}/config
NAME=${UNSET_VAR:-default}  # fallback if unset or empty

# export prefix is accepted and ignored
export API_KEY=secret
```

### Substitution

- `$VAR` and `${VAR}` — resolved against variables defined earlier in the file, then the inherited environment
- `${VAR:-default}` — uses the default value if the variable is unset or empty
- Substitution applies to unquoted and double-quoted values
- Single-quoted values are always literal

## How it works

envwith uses `syscall.Exec` to replace itself with the child process. There is no wrapper process — the child gets the PID, handles signals directly, and its exit code propagates naturally.

## License

MIT

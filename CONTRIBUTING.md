# Contributing

Bug reports and pull requests are welcome.

## Reports

Include `pear version` and `pear status`. Remove `api_key`, `access_token`, `refresh_token`, and anything under `extra` before you paste config.

Name the path you called (`/v1/chat/completions`, `/v1/messages`, or `/v1/responses`) and the `model` value. Leave the prompt out when it is private.

## Changes

From a clone:

```bash
mise install
mise run check
```

`mise run check` runs `go vet ./...` and `go build ./...`. In the pull request, say which request path or routing case you tried.

# npmreleaser
Release your Go package on NPM


## Build & Publish  

**With NPM**  
```sh
npx @sprisa/npmreleaser build --help
```

**With Go**  
```sh
go run github.com/sprisa/npmreleaser@latest build --help
```

- Platform packages are published sequentially, with interactive npm authentication available for each.
- The main package is published last, only after every platform package succeeds.
- A failed publish stops the release and reports the package directory; generated launchers preserve child exit codes and fail clearly if a platform package is missing.

## Environment templates

JSON string values support Go templates with environment variables:

```json
{
  "package.json": {
    "name": "example-cli",
    "version": "{{ .Env.VERSION }}"
  },
  "bin": "example-cli",
  "platforms": ["linux/amd64", "darwin/arm64"]
}
```

```sh
VERSION=0.1.0 npmreleaser build
```

- Templates work in nested package metadata, arrays, and other string values such as `bin` and `gobuild`.
- Missing environment variables or malformed templates fail before building or publishing.
- Quotes/newlines in values are JSON-escaped, and the source configuration is not rewritten.

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

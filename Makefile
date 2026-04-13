APP    = gomario
ENTRY  = assets/js/inc/main.js
BUNDLE = assets/js/bundle.js

.PHONY: all build js go dev clean

all: build

## build: compile JS bundle (minified) then Go binary
build: js go

## js: bundle and minify assets/js/inc/ with esbuild
js:
	npm run build

## go: compile the Go binary to ./tmp/$(APP)
go:
	go build -o ./tmp/$(APP) .

## dev: run esbuild in watch mode (background) + Go dev server
dev:
	npm run dev & go run .

## clean: remove build artefacts
clean:
	rm -f $(BUNDLE) $(BUNDLE).map ./tmp/$(APP)

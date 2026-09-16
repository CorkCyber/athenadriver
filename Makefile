.PHONY: build test vet cover lint athenareader examples clean

build:
	go build ./go/...

test:
	go test -race ./go/...

vet:
	go vet ./go/...

cover:
	go test -race -coverprofile=cover.out -coverpkg=./go/... ./go/...
	go tool cover -html=cover.out -o cover.html

# lint runs vet + gofmt -s. golint (golang.org/x/lint) was archived in 2021;
# upgrade to staticcheck if a richer linter is needed.
lint: vet
	@out="$$(gofmt -d -s ./go ./examples ./scope)"; \
	if [ -n "$$out" ]; then echo "$$out"; echo "gofmt -s found unformatted files"; exit 1; fi

athenareader:
	$(MAKE) -C athenareader build

examples:
	$(MAKE) -C examples build

clean:
	rm -f cover.out cover.html

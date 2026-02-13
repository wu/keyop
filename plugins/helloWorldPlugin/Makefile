.PHONY: build clean

PLUGIN_NAME=helloWorldPlugin.so

build:
	go mod tidy
	go build -buildmode=plugin -o $(PLUGIN_NAME) main.go

clean:
	rm -f $(PLUGIN_NAME)

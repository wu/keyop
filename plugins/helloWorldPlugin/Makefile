.PHONY: build clean

PLUGIN_NAME=helloWorldPlugin.so

build:
	go build -buildmode=plugin -o $(PLUGIN_NAME) main.go

clean:
	rm -f $(PLUGIN_NAME)

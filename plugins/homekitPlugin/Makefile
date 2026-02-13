.PHONY: build clean

PLUGIN_NAME=homekitPlugin.so

build:
	go build -buildmode=plugin -o $(PLUGIN_NAME) main.go

clean:
	rm -f $(PLUGIN_NAME)

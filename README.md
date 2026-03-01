# keyop

Project Under Construction!

Keyop is a simple, yet powerful framework for observing, predicting, and reacting to events. Think of it as a
lightweight and easy-to-use nerve center for your IoT projects, data pipelines, or anything else that generates
or reacts to interesting events.

Project Goals

* Deploy as a standalone instance or scale out to a distributed mesh, no fuss.
* Support for a variety of IOT sensors and data sources.
* Trigger custom notifications and reactions based on observations and predictions.
* Agents can react offline when disconnected from the network.
* Integration with Apple Homekit.
* Terminal UI - status HUD to monitor the state of the system.
* Data Privacy - you own your data, choose what you share.

Join the project to help make event-driven intelligence accessible to everyone!

## Build

Build the main binaries (no plugins):

```zsh
make build
```

Build plugins using the default plugin list:

```zsh
make plugins
```

Build only specific plugins:

```zsh
make build-plugins PLUGINS="rgbMatrix helloWorldPlugin"
```

Build the main binaries and also build specific plugins:

```zsh
make build PLUGINS="rgbMatrix helloWorldPlugin"
```

Note: The `rgbMatrix` plugin only builds on Raspberry Pi (Linux on ARM).

## Docker

The project provides two Docker build flows:

- `Dockerfile.prebuilt` produces a minimal image from a prebuilt binary (used by the Makefile helper).
- `Dockerfile.debug` produces a small Debian-based debug image (includes a shell and basic tools) to inspect files and
  debug runtime issues.

Important: the web UI runs on port 8823. Both Dockerfiles expose port 8823. When running containers map that port to the
host with `-p 8823:8823` to access the web UI.

Plugin inclusion

If an external plugin repository exists at `../keyop-webUiPlugin` (sibling to this repo), the build helper will copy it
into `plugins/webUiPlugin`, patch its `go.mod` (so `replace keyop => ../..` resolves), build it, and include the
resulting `webUiPlugin.so` into the image under `/.keyop/plugins/webUiPlugin/webUiPlugin.so` (and `/root/.keyop/...`
when applicable).

The plugin configuration files from `example-conf` are copied into the image at `/root/.keyop/conf` and `/.keyop/conf`.
The build script also adjusts the `plugins.yaml` and `plugin-webui.yaml` in the image context so the plugin points to
the in-container `.so` path.

In-container plugin path

- The plugin .so used by the image is placed at: `.keyop/plugins/webUiPlugin/webUiPlugin.so` (relative to the root of
  the filesystem inside the container). The build script updates:
  - `plugins.yaml` to use `path: .keyop/plugins/webUiPlugin/webUiPlugin.so`
  - `plugin-webui.yaml` to add a `soPath: .keyop/plugins/webUiPlugin/webUiPlugin.so` under `config:` if not present.

How to build

- Build a normal image (prebuilt final image):

```bash
make docker-build DOCKER_IMAGE=myuser/keyop DOCKER_TAG=local
```

- Build a debug image (Debian with shell/tools) for interactive debugging:

```bash
make docker-build-debug DOCKER_IMAGE=myuser/keyop-debug DOCKER_TAG=local
# or call the script directly and specify Dockerfile.debug
./scripts/docker-build.sh myuser/keyop-debug local Dockerfile.debug
```

How to run and inspect config (debug image recommended)

- Start an interactive shell in the debug image (recommended):

```bash
# map port 8823 and open a shell
docker run --rm -it -p 8823:8823 --entrypoint /bin/bash myuser/keyop-debug:local
# inside the container you can inspect
ls -la /root/.keyop/conf
ls -la /.keyop/conf
cat /root/.keyop/conf/plugin-webui.yaml
cat /root/.keyop/conf/plugins.yaml
ls -la /root/.keyop/plugins/webUiPlugin
```

- If using the minimal image (no shell), you can still inspect files by copying them out:

```bash
docker create --name tmpkeyop myuser/keyop:local
docker cp tmpkeyop:/root/.keyop/conf ./conf-inspect-root
docker cp tmpkeyop:/.keyop/conf ./conf-inspect-alt || true
docker rm tmpkeyop
ls -la conf-inspect-root
ls -la conf-inspect-alt
```

Notes

- The build helper script is `./scripts/docker-build.sh`. It will detect `../keyop-webUiPlugin`, patch `go.mod` replace
  directives, run `make build` (including the plugin), and prepare a Docker context that contains the `keyop` binary,
  the `example-conf` files, and the plugin `.so` at `.keyop/plugins/webUiPlugin/webUiPlugin.so`.
- If Docker is not available locally, the script still produces the build artifacts and will skip the `docker build`
  step.

If you'd like, I can also add a small smoke-test step to the CI workflow that pulls the image and checks that
`/root/.keyop/conf/plugin-webui.yaml` exists and contains the expected `soPath` entry.

# Shorter names for what ./build.sh does. The script is where the work is, and
# where it is documented: `make help`, or `./build.sh --help`.

.PHONY: all build install app cli test docs clean release help

all: build

build: ; ./build.sh
install: ; ./build.sh --install
app: ; ./build.sh --app
cli: ; ./build.sh --cli
test: ; ./build.sh --test
docs: ; ./build.sh --docs
clean: ; ./build.sh --clean
release: ; ./release.sh
help: ; @./build.sh --help

# Installing dr

dr is one binary. It runs on macOS and Linux, on Intel and ARM, and needs git. To ask and to fix, it also runs the CLI of the agent that wrote the lines: `claude`, `codex`, `omp` or `pi`.

## Homebrew

```sh
brew install jinmugo/tap/dr
```

This installs the latest release from the [JinmuGo/homebrew-tap](https://github.com/JinmuGo/homebrew-tap) tap, on macOS and Linux. `brew upgrade dr` updates it and `brew uninstall dr` removes it.

The release binary is not signed by Apple. Homebrew marks what it downloads as quarantined, and macOS refuses to run an unsigned binary with that mark, so the cask removes it after installing.

## Install script

```sh
curl -fsSL https://dr.jinmugo.com/install.sh | sh
```

The script:

1. picks the release archive for your system from `uname -s` and `uname -m`;
2. downloads it and the release's `checksums.txt` from GitHub;
3. stops if the archive's SHA-256 does not match;
4. puts `dr` in `~/.local/bin`, and says so if that directory is not on your `PATH`.

Options go after `sh -s --`:

```sh
curl -fsSL https://dr.jinmugo.com/install.sh | sh -s -- --version 0.1.0 --dir ~/bin
```

| Option | Environment | Default |
| --- | --- | --- |
| `--version <version>` | `DR_VERSION` | the latest release |
| `--dir <directory>` | `DR_INSTALL_DIR` | `~/.local/bin` |
| | `DR_DOWNLOAD_URL` | GitHub; set it to a mirror that holds the release files |

A directory you cannot write to, such as `/usr/local/bin`, needs `sudo sh` in place of `sh`. Running the script again updates dr. To remove dr, delete the binary, for example `rm ~/.local/bin/dr`.

To read the script before you run it, open <https://dr.jinmugo.com/install.sh>. Its source is [site/install.sh](../site/install.sh).

## Nix

```sh
nix profile install github:JinmuGo/dr
```

To run dr once without installing it:

```sh
nix run github:JinmuGo/dr
```

The flake builds dr from source and runs its tests, and needs the `nix-command` and `flakes` features. A flake build is not a release, so `dr --version` names the commit it was built from, such as `0-unstable-1a2b3c4`. In a flake of your own, add `github:JinmuGo/dr` as an input and use `inputs.dr.packages.${system}.default`.

## Go

```sh
go install github.com/JinmuGo/dr@latest
```

This needs Go 1.26 or later and puts dr in `$(go env GOPATH)/bin`.

## From source

```sh
git clone https://github.com/JinmuGo/dr
cd dr
go build -o dr .
```

## Releases

Each [release](https://github.com/JinmuGo/dr/releases) has an archive per system, `dr_Darwin_arm64.tar.gz`, `dr_Darwin_x86_64.tar.gz`, `dr_Linux_arm64.tar.gz` and `dr_Linux_x86_64.tar.gz`, and a `checksums.txt`. To check a download by hand, run this next to it:

```sh
shasum -a 256 -c checksums.txt --ignore-missing   # macOS
sha256sum -c checksums.txt --ignore-missing       # Linux
```

`dr --version` prints the version you have.

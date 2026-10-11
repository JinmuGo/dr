{
  description = "dr: diff review with the AI that wrote it";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      # A flake build is not a release, so its version names the commit it was built from.
      version = "0-unstable-${self.shortRev or self.dirtyShortRev or "dirty"}";
    in
    {
      packages = forAllSystems (pkgs: rec {
        dr = pkgs.buildGoModule {
          pname = "dr";
          inherit version;
          src = self;
          vendorHash = "sha256-Zz0wyQDiO3Na1XIV59t01cJTZ6qCT9/EG6e38ShlnVM=";
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];
          nativeCheckInputs = [ pkgs.git ];
          meta = {
            description = "Diff review with the AI that wrote it";
            homepage = "https://dr.jinmugo.com";
            license = pkgs.lib.licenses.mit;
            mainProgram = "dr";
          };
        };
        default = dr;
      });

      apps = forAllSystems (pkgs: {
        default = {
          type = "app";
          program = pkgs.lib.getExe self.packages.${pkgs.stdenv.hostPlatform.system}.dr;
          meta.description = "Run dr";
        };
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [
            pkgs.go
            pkgs.golangci-lint
            pkgs.goreleaser
            pkgs.git
          ];
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt);
    };
}

{
  description = "laneway: a terminal board for Jira";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAll = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = self.shortRev or "dev";
    in
    {
      packages = forAll (pkgs: rec {
        laneway = pkgs.buildGoModule {
          pname = "laneway";
          inherit version;
          src = self;
          vendorHash = "sha256-Am1xDQKXp5TVk047wrFh9Z1Gxe0mXqN2MOadi9dAW28=";
          env.CGO_ENABLED = 0;
          ldflags = [ "-s" "-w" "-X main.version=${version}" ];
          subPackages = [ "." ];
          doCheck = false;
          meta = {
            description = "A terminal board for Jira";
            homepage = "https://github.com/cornedor/laneway";
            license = pkgs.lib.licenses.mit;
            mainProgram = "laneway";
          };
        };
        default = laneway;
      });
    };
}

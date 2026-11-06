"""Sign a prepared bundle inside-out; never export keys or change trust policy."""
from dataclasses import dataclass
import os
from pathlib import Path
import re
import subprocess


MACHO = {bytes.fromhex(v) for v in ("feedface", "cefaedfe", "feedfacf", "cffaedfe", "cafebabe", "bebafeca", "cafebabf", "bfbafeca")}


@dataclass(frozen=True)
class SigningConfiguration:
    identity: str = "-"
    team: str = ""
    developer_id: bool = False

    @property
    def mode(self):
        return "adhoc" if self.identity == "-" else "team"


def output(*args):
    return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT)


def resolve_configuration(environment=None):
    env = os.environ if environment is None else environment
    requested = env.get("GOCONNECT_SIGN_IDENTITY", "").strip()
    team = env.get("GOCONNECT_SIGN_TEAM", "").strip()
    required = env.get("GOCONNECT_REQUIRE_TEAM_SIGN") == "1" or bool(requested) or bool(team)
    if not required:
        return SigningConfiguration()
    if team and not re.fullmatch(r"[A-Z0-9]{10}", team):
        raise ValueError("GOCONNECT_SIGN_TEAM must be a ten-character TeamIdentifier")
    identities = re.findall(r'([A-F0-9]{40}) "((?:Apple Development|Developer ID Application):[^"\n]+)"',
                            output("security", "find-identity", "-v", "-p", "codesigning"))
    choices = [(fingerprint, name) for fingerprint, name in identities
               if not requested or requested == fingerprint or requested == name]
    if len(choices) != 1:
        raise ValueError("Select exactly one existing Apple Development or Developer ID Application identity using GOCONNECT_SIGN_IDENTITY")
    fingerprint, name = choices[0]
    # OU comes from the certificate, not the personal identifier in its CN.
    pem = subprocess.check_output(["security", "find-certificate", "-c", name, "-p"])
    subject = subprocess.check_output(["openssl", "x509", "-noout", "-subject", "-nameopt", "RFC2253"], input=pem).decode()
    match = re.search(r"(?:^|,)OU=([A-Z0-9]{10})(?:,|$)", subject)
    if not match or (team and team != match.group(1)):
        raise ValueError("Signing certificate TeamIdentifier does not match GOCONNECT_SIGN_TEAM")
    return SigningConfiguration(fingerprint, match.group(1), name.startswith("Developer ID Application:"))


def macho_files(app):
    result = []
    for path in app.rglob("*"):
        if path.is_symlink() or not path.is_file():
            continue
        with path.open("rb") as stream:
            if stream.read(4) in MACHO:
                result.append(path)
    return sorted(result, key=lambda p: (-len(p.parts), str(p)))


def signing_order(app):
    bundles = [p for p in app.rglob("*") if not p.is_symlink() and p.is_dir()
               and p.suffix in (".app", ".xpc", ".framework", ".appex")]
    return macho_files(app) + sorted(bundles, key=lambda p: (-len(p.parts), str(p))) + [app]


def sign_bundle(app, configuration):
    app = Path(app)
    if app.suffix != ".app" or not (app / "Contents/Info.plist").is_file():
        raise ValueError("Expected one complete prepared app bundle")
    for target in signing_order(app):
        command = ["codesign", "--force", "--sign", configuration.identity,
                   "--timestamp" if configuration.developer_id else "--timestamp=none"]
        if configuration.mode == "team" or target.name == "GoConnectTransport":
            command += ["--options", "runtime"]
        # No preserve-metadata: upstream Autoupdate application identifiers are
        # not our entitlements. This app is not sandboxed and needs no exception.
        subprocess.run(command + [str(target)], check=True, capture_output=True)
    subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)], check=True)
    if configuration.mode == "team":
        for target in signing_order(app):
            metadata = output("codesign", "-dvv", str(target))
            if "TeamIdentifier=" + configuration.team not in metadata or "runtime" not in metadata:
                raise ValueError("Missing expected team or hardened runtime: " + str(target.relative_to(app)))
            entitlements = output("codesign", "-d", "--entitlements", ":-", str(target))
            if "get-task-allow" in entitlements:
                raise ValueError("Debug entitlement in team-signed bundle: " + str(target.relative_to(app)))
    print("Signing verified:", configuration.mode, configuration.team or "no team",
          "Developer ID" if configuration.developer_id else "development" if configuration.mode == "team" else "local")

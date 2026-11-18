#!/usr/bin/env python3
"""Brand Zitadel's hosted login to match Quill's app (idempotent).

Zitadel's login pages are served by Zitadel, so they cannot share Quill's CSS; its
label policy (colours, logo, font, theme) and login texts get them most of the
way. Run it after any change to Quill's palette, and after restoring a Zitadel
database into a fresh instance. It never prints the token.

usage: zitadel-branding.py [--font PATH]
env:   ZITADEL_ISSUER (default https://auth.black-tulip.nl), ZITADEL_MANAGEMENT_TOKEN
       (an IAM_OWNER PAT; Secret Manager /quill/app/app-zitadel-management-token)

Quill tokens come from frontend/app/globals.css (--bg, --text, --accent, --red);
keep them in sync here.
"""
import json
import os
import sys
import uuid
import urllib.error
import urllib.request

ISSUER = os.environ.get("ZITADEL_ISSUER", "https://auth.black-tulip.nl").rstrip("/")
TOKEN = os.environ["ZITADEL_MANAGEMENT_TOKEN"]
HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, "..", "..", ".."))

# Quill's dark palette, aligned with the landing page (blacktulipweb): near-black
# base, blue accent. The glows / card come from cloudflare/zitadel-login.css.
PALETTE = {
    "primaryColor": "#4f8ff0",   # accent
    "backgroundColor": "#0b0b0e",  # landing --bg
    "fontColor": "#eceae7",      # landing --text-hi
    "warnColor": "#f0506e",      # --red
}

# Login texts (language "en"): what a person reads on Zitadel's pages.
LOGIN_TEXTS = {
    "loginText": {
        "title": "Sign in to Quill",
        "description": "Use your Quill account or your company login.",
        "loginNameLabel": "Email or username",
    },
    "passwordText": {
        "title": "Welcome back",
        "description": "Enter your password to continue.",
    },
}


def call(method, path, body=None, raw=None, content_type="application/json"):
    data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
    req = urllib.request.Request(ISSUER + path, data=data, method=method, headers={
        "Authorization": "Bearer " + TOKEN,
        "Content-Type": content_type,
        "User-Agent": "quill-operator/1.0",  # Cloudflare rejects Python's default UA (error 1010)
    })
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            payload = r.read()
            return r.status, (json.loads(payload) if payload else {})
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace")[:400]


def upload(path, filename, content, mime):
    boundary = uuid.uuid4().hex
    body = (
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{filename}\"\r\n"
        f"Content-Type: {mime}\r\n\r\n"
    ).encode() + content + f"\r\n--{boundary}--\r\n".encode()
    return call("POST", path, raw=body, content_type=f"multipart/form-data; boundary={boundary}")


def main() -> int:
    font = None
    if "--font" in sys.argv:
        font = sys.argv[sys.argv.index("--font") + 1]
    else:
        guess = os.path.join(ROOT, "frontend", "node_modules", "geist", "dist", "fonts", "geist-sans", "Geist-Regular.ttf")
        font = guess if os.path.exists(guess) else None

    policy = dict(PALETTE)
    for k in list(PALETTE):
        policy[k + "Dark"] = PALETTE[k]  # one dark theme: light and dark variants identical
    policy.update({"themeMode": "THEME_MODE_DARK", "disableWatermark": True, "hideLoginNameSuffix": True})
    st, out = call("PUT", "/admin/v1/policies/label", policy)
    if st not in (200, 400):  # 400 "not changed" on a re-run
        print("label policy:", st, out, file=sys.stderr)
        return 1
    print("label policy:", "updated" if st == 200 else "unchanged")

    tile = open(os.path.join(ROOT, "frontend", "public", "favicon.svg"), "r").read()
    # The login logo is the gold glyph alone at a fixed size (the dark tile of the
    # favicon blends into the page and renders huge); the browser-tab icon keeps the tile.
    glyph = "\n".join(l for l in tile.splitlines() if "<rect" not in l)
    glyph = glyph.replace("<svg ", '<svg width="56" height="56" ', 1)
    for kind, svg in (("logo", glyph), ("logo/dark", glyph), ("icon", tile), ("icon/dark", tile)):
        st, out = upload(f"/assets/v1/instance/policy/label/{kind}", "quill.svg", svg.encode(), "image/svg+xml")
        print(f"{kind}:", st if st != 200 else "ok", "" if st == 200 else out)
    if font:
        st, out = upload("/assets/v1/instance/policy/label/font", "Geist-Regular.ttf", open(font, "rb").read(), "font/ttf")
        print("font:", st if st != 200 else "ok", "" if st == 200 else out)
    else:
        print("font: skipped (install the frontend dependencies or pass --font)")

    st, out = call("POST", "/admin/v1/policies/label/_activate", {})
    print("activate:", st if st != 200 else "ok", "" if st == 200 else out)

    # SetCustomLoginTexts takes the text groups at the top level (a nested
    # "customText" is silently ignored).
    st, out = call("PUT", "/admin/v1/text/login/en", LOGIN_TEXTS)
    print("login texts:", st if st != 200 else "ok", "" if st == 200 else out)

    # Someone who skips the 2-factor setup page is asked again after
    # mfaInitSkipLifetime (default 30 days). Ten years: offered once, never nagged.
    st, cur = call("GET", "/admin/v1/policies/login")
    if st == 200:
        pol = {k: v for k, v in cur["policy"].items() if k not in ("details", "isDefault")}
        pol["mfaInitSkipLifetime"] = "315360000s"
        # Accounts are created by Quill's own sign-up form (handle + email + password);
        # Zitadel's registration page (first/last name, no username) is turned off.
        pol["allowRegister"] = False
        st, out = call("PUT", "/admin/v1/policies/login", pol)
        print("login policy (mfa skip, no self-registration):", "ok" if st == 200 else ("unchanged" if st == 400 else f"{st} {out}"))
    return 0


if __name__ == "__main__":
    sys.exit(main())

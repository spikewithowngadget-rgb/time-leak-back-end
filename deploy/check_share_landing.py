#!/usr/bin/env python3
"""Exercise the deployed Caddy landing template with a local mock API.

Uses only the Python standard library and the installed caddy binary. Neither
the running Caddy service nor the application service is changed.
"""

import base64
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import threading
import time
from html.parser import HTMLParser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


DEPLOY = Path(__file__).resolve().parent
SECRET = "landing-test-secret-must-never-appear"
UNSAFE_TITLE = '{{ env "SHARE_LANDING_TEST_SECRET" }} & <script>alert("x")</script>'
VALID_TOKEN = "Valid1234567"
UNSAFE_TOKEN = "Unsafe123456"
ERROR_TOKENS = {"Unknown12345": 404, "Expired12345": 410, "Unavail12345": 503}
BROKEN_TOKEN = "Broken123456"


def page_data(token, title="Важный момент", status=200):
    return {
        "Status": status,
        "Title": title,
        "Description": "Заметка в TimeLeak • 2026-10-22",
        "Image": "https://timeleak.kz/api/v1/notes/shared/" + token + "/preview?sig=test",
        "URL": "https://timeleak.kz/share/" + token,
        "Date": "2026-10-22",
        "DeepLink": "timeleak://share/" + token,
        "AppStoreURL": "https://apps.apple.com/app/id6762095094",
        "PlayStoreURL": "https://play.google.com/store/apps/details?id=kz.timeleak.app",
        "Message": "Откройте заметку в приложении.",
    }


class MockBackend(BaseHTTPRequestHandler):
    def do_GET(self):
        path = self.path.split("?", 1)[0]
        prefix = "/api/v1/notes/shared/"
        if path.startswith(prefix) and path.endswith("/landing"):
            token = path[len(prefix):-len("/landing")]
            if token == BROKEN_TOKEN:
                self.reply(503, b'{"error":"upstream temporarily unavailable"}', "application/json")
                return
            status = ERROR_TOKENS.get(token, 200 if token in (VALID_TOKEN, UNSAFE_TOKEN) else 404)
            title = UNSAFE_TITLE if token == UNSAFE_TOKEN else "Важный момент"
            payload = json.dumps(page_data(token, title, status), ensure_ascii=False).encode()
            self.reply(200, base64.b64encode(payload), "text/plain; charset=utf-8")
        elif path == prefix + VALID_TOKEN:
            self.reply(200, b'{"id":1,"title":"API JSON remains"}', "application/json")
        else:
            self.reply(200, b"admin-ok", "text/plain; charset=utf-8")

    def reply(self, status, payload, content_type):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *_args):
        pass


class MetadataParser(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.meta = {}
        self.anchors = {}
        self.title = ""
        self.in_title = False

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "meta":
            self.meta[attrs.get("property", attrs.get("name", ""))] = attrs.get("content")
        elif tag == "a" and attrs.get("id"):
            self.anchors[attrs["id"]] = attrs.get("href")
        elif tag == "title":
            self.in_title = True

    def handle_endtag(self, tag):
        if tag == "title":
            self.in_title = False

    def handle_data(self, data):
        if self.in_title:
            self.title += data


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def run_checks(base_url):
    checks = 0

    def request(path, method="GET"):
        try:
            response = urlopen(Request(base_url + path, method=method), timeout=5)
        except HTTPError as response_error:
            response = response_error
        with response:
            return response.status, response.headers, response.read()

    def html_response(path, expected_status):
        nonlocal checks
        status, headers, payload = request(path)
        require(status == expected_status, f"{path}: expected {expected_status}, got {status}")
        require(headers.get_content_type() == "text/html", f"{path}: did not return HTML")
        text = payload.decode("utf-8")
        require("<!doctype html>" in text.lower(), f"{path}: missing HTML document")
        require("TimeLeak" in text, f"{path}: missing human-readable error/landing")
        require(SECRET not in text, f"{path}: template environment secret leaked")
        checks += 1
        return text

    text = html_response("/share/" + VALID_TOKEN, 200)
    page = MetadataParser()
    page.feed(text)
    require(page.title == "Важный момент — TimeLeak", "valid landing title differs")
    require(page.meta.get("og:title") == "Важный момент", "missing note OG title")
    require(page.meta.get("og:description") == page_data(VALID_TOKEN)["Description"], "missing OG description")
    require(page.meta.get("og:image") == page_data(VALID_TOKEN)["Image"], "missing OG image")
    require(page.meta.get("og:url") == page_data(VALID_TOKEN)["URL"], "missing canonical OG URL")
    require(page.anchors.get("open-app") == "timeleak://share/" + VALID_TOKEN, "wrong app deep link")

    text = html_response("/share/" + UNSAFE_TOKEN, 200)
    page = MetadataParser()
    page.feed(text)
    require(page.title == UNSAFE_TITLE + " — TimeLeak", "unsafe note title was evaluated or corrupted")
    require(page.meta.get("og:title") == UNSAFE_TITLE, "unsafe OG title was evaluated or corrupted")
    require("<script>alert" not in text and "&lt;script&gt;" in text, "note HTML was not escaped")
    require("{{ env" in text, "user-controlled template expression was evaluated")

    for token in ("short", "toolong1234567", "invalid!1234"):
        html_response("/share/" + token, 404)
    for token, expected_status in ERROR_TOKENS.items():
        html_response("/share/" + token, expected_status)

    status, headers, payload = request("/share/" + BROKEN_TOKEN)
    require(status >= 400, f"upstream HTTP 503 produced success status {status}")
    require(headers.get_content_type() == "text/html", "upstream HTTP error did not become HTML")
    require(b"<!doctype html>" in payload.lower(), "upstream HTTP error has no human HTML page")
    require(b'"error"' not in payload, "raw upstream JSON was exposed")
    checks += 1

    for asset, content_type in (("landing.css", "text/css"), ("landing.js", "text/javascript")):
        status, headers, payload = request("/share-assets/" + asset)
        require(status == 200 and payload, f"asset {asset} missing")
        accepted_types = (content_type, "application/javascript") if asset.endswith(".js") else (content_type,)
        require(headers.get_content_type() in accepted_types, f"asset {asset}: unexpected MIME type")
        checks += 1

    status, _headers, payload = request("/share-assets/index.html")
    require(status == 404, "the Caddy template source is exposed through assets")
    require(b"httpInclude" not in payload, "raw Caddy template source leaked")
    checks += 1

    status, headers, payload = request("/api/v1/notes/shared/" + VALID_TOKEN)
    require(status == 200 and headers.get_content_type() == "application/json", "public note JSON routing changed")
    require(json.loads(payload)["title"] == "API JSON remains", "public note JSON response changed")
    checks += 1

    status, _headers, payload = request("/admin")
    require(status == 200 and payload == b"admin-ok", "fallback admin routing changed")
    checks += 1

    status, headers, payload = request("/share/" + VALID_TOKEN, "HEAD")
    require(status == 200 and headers.get_content_type() == "text/html", "HEAD landing failed")
    require(payload == b"", "HEAD response includes a body")
    checks += 1
    return checks


def main():
    caddy = shutil.which("caddy")
    if not caddy:
        raise SystemExit("caddy is required for this integration check")
    for token in (VALID_TOKEN, UNSAFE_TOKEN, BROKEN_TOKEN, *ERROR_TOKENS):
        require(len(token) == 12, f"test token must match the real token format: {token}")

    snippet = (DEPLOY / "Caddyfile").read_text().split("\ntimeleak.kz {", 1)[0]
    require("(share_landing)" in snippet, "share_landing Caddy snippet not found")
    snippet = snippet.replace("/srv/timeleak-share", str(DEPLOY / "share"))
    backend = ThreadingHTTPServer(("127.0.0.1", 0), MockBackend)
    backend_port = backend.server_address[1]
    threading.Thread(target=backend.serve_forever, daemon=True).start()
    caddy_port = free_port()
    base_url = f"http://127.0.0.1:{caddy_port}"
    environment = dict(os.environ, SHARE_LANDING_TEST_SECRET=SECRET)
    process = None
    try:
        with tempfile.TemporaryDirectory(prefix="timeleak-landing-check-") as directory:
            temporary = Path(directory)
            # Keep Caddy's autosave/storage outside the operator's real state.
            environment["XDG_CONFIG_HOME"] = str(temporary / "config")
            environment["XDG_DATA_HOME"] = str(temporary / "data")
            config = temporary / "Caddyfile"
            config.write_text(
                "{\n\tadmin off\n\tauto_https off\n}\n" + snippet +
                f"\n{base_url} {{\n\timport share_landing\n"
                "\t@sharingAPI path /api/v1/notes/shared/*\n"
                f"\thandle @sharingAPI {{\n\t\treverse_proxy 127.0.0.1:{backend_port}\n\t}}\n"
                f"\thandle {{\n\t\treverse_proxy 127.0.0.1:{backend_port}\n\t}}\n}}\n"
            )
            validation = subprocess.run(
                [caddy, "validate", "--config", str(config), "--adapter", "caddyfile"],
                capture_output=True, text=True, env=environment,
            )
            require(validation.returncode == 0, "Caddy config invalid:\n" + validation.stderr)
            with (temporary / "caddy.log").open("w+") as log:
                process = subprocess.Popen(
                    [caddy, "run", "--config", str(config), "--adapter", "caddyfile"],
                    stdout=log, stderr=subprocess.STDOUT, env=environment,
                )
                try:
                    for _attempt in range(100):
                        if process.poll() is not None:
                            raise RuntimeError("test Caddy stopped during startup")
                        try:
                            with urlopen(base_url + "/admin", timeout=.2):
                                break
                        except (URLError, TimeoutError):
                            time.sleep(.05)
                    else:
                        raise RuntimeError("test Caddy did not start")
                    count = run_checks(base_url)
                    print(f"OK: {count} Caddy integration responses checked; routing, escaping, HTML errors and HEAD passed.")
                except Exception:
                    log.flush()
                    log.seek(0)
                    print(log.read())
                    raise
                finally:
                    process.terminate()
                    process.wait(timeout=5)
                    process = None
    finally:
        if process is not None:
            process.terminate()
            process.wait(timeout=5)
        backend.shutdown()
        backend.server_close()


if __name__ == "__main__":
    main()

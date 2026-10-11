#!/usr/bin/env python3
"""Fake Slack, Microsoft Teams and SMTP server for the Crew Studio E2E suite (stdlib only, never contacts a real service).

HTTP (--port):
  POST /slack/hook/...           an incoming webhook: 200 "ok"
  POST /slack/api/chat.postMessage  needs "Authorization: Bearer xoxb-e2e-bot"; answers {"ok": true}
  POST /teams/...                a Teams workflow: 202
  POST /__fail?status=502        makes every webhook answer that status until POST /__ok
  GET  /__received               what the webhooks received ([{path, body}])
  GET  /__mail                   what the SMTP server accepted ([{from, to, data}])
  GET  /__health                 ok
SMTP (--smtp-port): plain SMTP without TLS or sign-in (Studio allows that only to this machine): EHLO, MAIL, RCPT,
DATA, RSET, NOOP, QUIT.
"""
import argparse
import json
import socketserver
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

BOT_TOKEN = "xoxb-e2e-bot"
LOCK = threading.Lock()
RECEIVED: list[dict] = []
MAIL: list[dict] = []
FAIL = {"status": 0}


class Hooks(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def _send(self, code, payload, content_type="application/json"):
        body = payload.encode() if isinstance(payload, str) else json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("content-type", content_type)
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = urlparse(self.path).path
        with LOCK:
            if path == "/__health":
                return self._send(200, {"status": "ok"})
            if path == "/__received":
                return self._send(200, list(RECEIVED))
            if path == "/__mail":
                return self._send(200, list(MAIL))
        self._send(404, {"error": "not found"})

    def do_POST(self):
        url = urlparse(self.path)
        length = int(self.headers.get("content-length") or 0)
        raw = self.rfile.read(length)
        if url.path == "/__fail":
            FAIL["status"] = int(parse_qs(url.query).get("status", ["502"])[0])
            return self._send(200, {"failing": FAIL["status"]})
        if url.path == "/__ok":
            FAIL["status"] = 0
            return self._send(200, {"failing": 0})
        try:
            body = json.loads(raw or b"{}")
        except ValueError:
            return self._send(400, "invalid_payload", "text/plain")
        with LOCK:
            RECEIVED.append({"path": url.path, "body": body})
            failing = FAIL["status"]
        if url.path == "/slack/api/chat.postMessage":
            if self.headers.get("Authorization") != "Bearer " + BOT_TOKEN:
                return self._send(200, {"ok": False, "error": "invalid_auth"})
            return self._send(200, {"ok": True, "channel": "C0E2E", "ts": "1.1"})
        if failing:
            return self._send(failing, "service_unavailable", "text/plain")
        if url.path.startswith("/teams/"):
            return self._send(202, "")
        if url.path.startswith("/slack/hook/"):
            return self._send(200, "ok", "text/plain")
        self._send(404, "no_service", "text/plain")


class SMTPHandler(socketserver.StreamRequestHandler):
    def reply(self, line):
        self.wfile.write((line + "\r\n").encode())

    def handle(self):
        self.reply("220 fake.smtp ESMTP")
        mail = {"from": "", "to": [], "data": ""}
        while True:
            line = self.rfile.readline()
            if not line:
                return
            verb, _, arg = line.decode(errors="replace").rstrip("\r\n").partition(" ")
            verb = verb.upper()
            if verb in ("EHLO", "HELO"):
                self.reply("250-fake.smtp")
                self.reply("250 8BITMIME")
            elif verb == "MAIL":
                mail = {"from": address(arg), "to": [], "data": ""}
                self.reply("250 ok")
            elif verb == "RCPT":
                mail["to"].append(address(arg))
                self.reply("250 ok")
            elif verb == "DATA":
                self.reply("354 end with .")
                mail["data"] = self.read_data()
                with LOCK:
                    MAIL.append(mail)
                self.reply("250 queued")
            elif verb in ("RSET", "NOOP"):
                self.reply("250 ok")
            elif verb == "QUIT":
                self.reply("221 bye")
                return
            else:
                self.reply("502 not implemented")

    def read_data(self):
        lines = []
        while True:
            line = self.rfile.readline().decode(errors="replace")
            if line in ("", ".\r\n", ".\n"):
                return "".join(lines)
            lines.append(line[1:] if line.startswith(".") else line)


def address(arg):
    value = arg.partition(":")[2].strip()
    if value.startswith("<"):
        value = value[1:].partition(">")[0]
    return value


class ThreadingSMTP(socketserver.ThreadingMixIn, socketserver.TCPServer):
    daemon_threads = True
    allow_reuse_address = True


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=8096)
    parser.add_argument("--smtp-port", type=int, default=8097)
    args = parser.parse_args()
    smtp = ThreadingSMTP(("127.0.0.1", args.smtp_port), SMTPHandler)
    threading.Thread(target=smtp.serve_forever, daemon=True).start()
    ThreadingHTTPServer(("127.0.0.1", args.port), Hooks).serve_forever()


if __name__ == "__main__":
    main()

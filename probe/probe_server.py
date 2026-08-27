#!/usr/bin/env python3
"""Tiny static server that also collects POST /result into ./result.txt"""
import http.server, os, sys

BASE = os.path.dirname(os.path.abspath(__file__))
RESULT = os.path.join(BASE, "result.txt")

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        if self.path.startswith("/player-test.html"):
            with open(os.path.join(BASE, "player-test.html"), "rb") as f:
                body = f.read()
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_response(404)
        self.end_headers()

    def do_POST(self):
        if self.path.startswith("/result"):
            n = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(n).decode("utf-8", "replace")
            with open(RESULT, "a") as f:
                f.write(body + "\n---\n")
            self.send_response(200)
            self.end_headers()
            return
        self.send_response(404)
        self.end_headers()

if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8898
    http.server.HTTPServer(("0.0.0.0", port), H).serve_forever()

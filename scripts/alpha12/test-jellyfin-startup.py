"""Exercise the DVR harness against the statuses returned while Jellyfin starts."""
import io
import json
import unittest
from contextlib import ExitStack
from unittest.mock import patch
from urllib.error import HTTPError

import jellyfin


class StartupReadiness(unittest.TestCase):
    def setUp(self):
        self.attempts = []
        self.readiness = [503, 200]
        self.ready = False
        self.config_write_status = 204
        stack = ExitStack()
        self.addCleanup(stack.close)
        stack.enter_context(patch.object(jellyfin.http, "open", side_effect=self.reply))
        stack.enter_context(patch.object(jellyfin.time, "sleep"))

    def reply(self, req, timeout):
        route = (req.get_method(), req.selector)
        self.attempts.append(route)
        status = 200
        body = {}
        if route == ("GET", "/System/Info/Public"):
            body = {"Version": "12.4.0"}
        elif route == ("GET", "/Startup/Configuration"):
            status = self.readiness.pop(0)
            self.ready = status == 200
            body = {"UICulture": "en-GB"}
        elif route == ("POST", "/Startup/Configuration"):
            status = self.config_write_status if self.ready else 503
        elif route == ("POST", "/Users/AuthenticateByName"):
            body = {"AccessToken": "test-only"}
        if status >= 400:
            raise HTTPError(req.full_url, status, "fixture", {}, None)
        return io.BytesIO(json.dumps(body).encode() if status == 200 else b"")

    def test_waits_for_wizard_before_first_write(self):
        jellyfin.jellyfin_setup("http://jellyfin.example")
        self.assertEqual(self.attempts[:3], [
            ("GET", "/Startup/Configuration"),
            ("GET", "/Startup/Configuration"),
            ("POST", "/Startup/Configuration"),
        ])
        self.assertEqual(len(self.attempts), len(set(self.attempts)) + 1)

    def test_permanent_wizard_error_stops_immediately(self):
        self.readiness = [403]
        with self.assertRaisesRegex(RuntimeError, "HTTP 403"):
            jellyfin.jellyfin_setup("http://jellyfin.example")
        self.assertEqual(self.attempts, [("GET", "/Startup/Configuration")])

    def test_ambiguous_write_failure_is_not_retried(self):
        self.readiness = [200]
        self.config_write_status = 503
        with self.assertRaises(HTTPError):
            jellyfin.jellyfin_setup("http://jellyfin.example")
        self.assertEqual(self.attempts, [
            ("GET", "/Startup/Configuration"), ("POST", "/Startup/Configuration")
        ])


if __name__ == "__main__":
    unittest.main()

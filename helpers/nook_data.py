"""Read and write this nook's data from its own server code.

A nook's pages use nook.js. This is the same thing for the code behind them, with the same rules:
shared collections are shared, a me/ collection belongs to one person, and a viewer can read but
not change shared data.

    import nook_data

    # For whoever is making this request. Pass the request's headers.
    me = nook_data.viewer(headers)
    me.create("me/notes", {"text": "mine"})
    me.list("notes")
    me.user            # {"email": ..., "name": ..., "role": ...}

    # With nobody on the other end, from a cron entry. Shared collections only.
    nook_data.list("notes")

Nothing here needs configuring: nookd puts NOOK_DATA_URL and NOOK_TOKEN in the environment.
"""

import json
import os
import urllib.error
import urllib.parse
import urllib.request

__all__ = ["Nook", "NookError", "viewer", "list", "get", "create", "update", "delete"]


class NookError(Exception):
    """A call the data API refused. .status is the HTTP status."""

    def __init__(self, status, message):
        super().__init__(message)
        self.status = status


class Nook:
    def __init__(self, as_token=None, user=None):
        self.base = os.environ.get("NOOK_DATA_URL", "")
        self.token = os.environ.get("NOOK_TOKEN", "")
        self.user = user or {}
        self._as = as_token

    def list(self, collection, limit=None, cursor=None, order=None, where=None):
        """Documents in a collection, newest last unless order='desc'."""
        q = []
        if limit:
            q.append(("limit", limit))
        if cursor:
            q.append(("cursor", cursor))
        if order:
            q.append(("order", order))
        # One where= per field, which is how the server reads them: they are and-ed together.
        # A single parameter would quietly match on the last field only and return too much.
        for field, value in (where or {}).items():
            q.append(("where", "%s:%s" % (field, value)))
        return self._call("GET", collection, params=q).get("documents", [])

    def get(self, collection, id):
        return self._call("GET", "%s/%s" % (collection, id))

    def create(self, collection, doc):
        return self._call("POST", collection, body=doc)

    def update(self, collection, id, fields):
        """Merges fields into the document; anything not named is left alone."""
        return self._call("PATCH", "%s/%s" % (collection, id), body=fields)

    def delete(self, collection, id):
        self._call("DELETE", "%s/%s" % (collection, id))

    def _call(self, method, path, body=None, params=None):
        if not self.base:
            raise NookError(0, "NOOK_DATA_URL is not set: this is not running as a nook")
        url = self.base + urllib.parse.quote(path)
        if params:
            url += "?" + urllib.parse.urlencode(params, doseq=True)
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(url, method=method, data=data)
        req.add_header("Authorization", "Bearer " + self.token)
        req.add_header("Content-Type", "application/json")
        if self._as:
            req.add_header("X-Nook-As", self._as)
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                raw = r.read()
                return json.loads(raw) if raw else {}
        except urllib.error.HTTPError as e:
            raw = e.read()
            try:
                message = json.loads(raw).get("error", raw.decode("utf-8", "replace"))
            except Exception:
                message = raw.decode("utf-8", "replace")
            raise NookError(e.code, message) from None


def _header(headers, name):
    """Headers come as a dict, a WSGI environ, or http.server's own object. Try each shape."""
    if headers is None:
        return ""
    for key in (name, name.lower(), "HTTP_" + name.upper().replace("-", "_")):
        try:
            value = headers.get(key)
        except AttributeError:
            return ""
        if value:
            return value
    return ""


def viewer(headers):
    """A Nook that acts for the person making this request.

    The proxy puts their identity in the request's headers, and X-Nook-As is what lets this read
    and write their data. Pass the headers through rather than an email address: a nook can act
    for someone who is using it, not for anyone it can name.
    """
    return Nook(
        as_token=_header(headers, "X-Nook-As"),
        user={
            "email": _header(headers, "X-Nook-User"),
            "name": _header(headers, "X-Nook-Name"),
            "role": _header(headers, "X-Nook-Role"),
        },
    )


# The nook acting as itself: shared collections only, for cron and anything with no viewer.
_self = Nook()
list = _self.list  # noqa: A001 - mirrors nook.js, where it is nook.data.list
get = _self.get
create = _self.create
update = _self.update
delete = _self.delete

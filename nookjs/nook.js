(function () {
  var base = "/_nook/data/";
  function req(method, path, body, q) {
    var url = base + path;
    if (q) {
      var parts = [];
      Object.keys(q).forEach(function (k) {
        if (q[k] === undefined || q[k] === null || q[k] === "") return;
        if (k === "where" && typeof q[k] === "object") {
          Object.keys(q[k]).forEach(function (f) { parts.push("where=" + encodeURIComponent(f + ":" + q[k][f])); });
        } else parts.push(encodeURIComponent(k) + "=" + encodeURIComponent(q[k]));
      });
      if (parts.length) url += "?" + parts.join("&");
    }
    return fetch(url, {
      method: method,
      headers: body ? { "Content-Type": "application/json" } : {},
      body: body ? JSON.stringify(body) : undefined,
      credentials: "same-origin"
    }).then(function (r) {
      return r.json().then(function (j) {
        if (!r.ok) { var e = new Error(j.error || r.statusText); e.status = r.status; throw e; }
        return j;
      });
    });
  }
  var user = { email: null, name: null, role: null, signedIn: false, canEdit: false, signInUrl: null, nook: null, owner: null, mode: null };
  var ready = fetch("/_nook/me", { credentials: "same-origin" }).then(function (r) { return r.json(); }).then(function (m) {
    user.email = m.email || null; user.name = m.name || null; user.role = m.role || null; user.signedIn = !!m.email;
    user.canEdit = m.role === "editor" || m.role === "owner"; user.signInUrl = m.signin_url;
    user.nook = m.nook; user.owner = m.owner; user.mode = m.mode;
    return user;
  });
  window.nook = {
    user: user,
    ready: ready,
    signIn: function () { window.location.href = user.signInUrl || "/_nook/me"; },
    data: {
      list:   function (c, q)     { return req("GET", c, null, q); },
      get:    function (c, id)    { return req("GET", c + "/" + id); },
      create: function (c, d)     { return req("POST", c, d); },
      update: function (c, id, d) { return req("PATCH", c + "/" + id, d); },
      remove: function (c, id)    { return req("DELETE", c + "/" + id); },
      exportUrl: function (c, format) { return "/_nook/export/" + c + "?format=" + (format || "json"); }
    }
  };
})();

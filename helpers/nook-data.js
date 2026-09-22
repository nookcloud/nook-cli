// Read and write this nook's data from its own server code.
//
// A nook's pages use nook.js. This is the same thing for the code behind them, with the same
// rules: shared collections are shared, a me/ collection belongs to one person, and a viewer can
// read but not change shared data.
//
//   const nook = require('./nook-data.js')
//
//   // For whoever is making this request. Pass the request's headers.
//   const me = nook.viewer(req.headers)
//   await me.create('me/notes', { text: 'mine' })
//   await me.list('notes')
//   me.user                      // { email, name, role }
//
//   // With nobody on the other end, from a cron entry. Shared collections only.
//   await nook.list('notes')
//
// Nothing here needs configuring: nookd puts NOOK_DATA_URL and NOOK_TOKEN in the environment.
// This file is CommonJS, which is what Node uses for a .js file in a folder with no package.json.
// If yours sets "type": "module", rename it to nook-data.cjs and require that.

class NookError extends Error {
  constructor(status, message) {
    super(message)
    this.name = 'NookError'
    this.status = status
  }
}

class Nook {
  constructor(asToken, user) {
    this.base = process.env.NOOK_DATA_URL || ''
    this.token = process.env.NOOK_TOKEN || ''
    this.user = user || {}
    this._as = asToken
  }

  // Documents in a collection, newest last unless order is 'desc'.
  async list(collection, opts = {}) {
    const q = new URLSearchParams()
    if (opts.limit) q.set('limit', opts.limit)
    if (opts.cursor) q.set('cursor', opts.cursor)
    if (opts.order) q.set('order', opts.order)
    for (const [field, value] of Object.entries(opts.where || {})) q.set('where', `${field}:${value}`)
    const out = await this._call('GET', collection, null, q)
    return out.documents || []
  }

  get(collection, id) { return this._call('GET', `${collection}/${id}`) }
  create(collection, doc) { return this._call('POST', collection, doc) }
  // Merges fields into the document; anything not named is left alone.
  update(collection, id, fields) { return this._call('PATCH', `${collection}/${id}`, fields) }
  async delete(collection, id) { await this._call('DELETE', `${collection}/${id}`) }

  async _call(method, path, body, query) {
    if (!this.base) throw new NookError(0, 'NOOK_DATA_URL is not set: this is not running as a nook')
    let url = this.base + encodeURI(path)
    if (query && [...query].length) url += '?' + query.toString()
    const headers = { Authorization: 'Bearer ' + this.token, 'Content-Type': 'application/json' }
    if (this._as) headers['X-Nook-As'] = this._as
    const resp = await fetch(url, {
      method,
      headers,
      body: body === undefined || body === null ? undefined : JSON.stringify(body),
    })
    const text = await resp.text()
    if (!resp.ok) {
      let message = text
      try { message = JSON.parse(text).error || text } catch {}
      throw new NookError(resp.status, message)
    }
    return text ? JSON.parse(text) : {}
  }
}

// Headers arrive as a plain object from node:http, or as a Headers from a framework.
function header(headers, name) {
  if (!headers) return ''
  if (typeof headers.get === 'function') return headers.get(name) || ''
  return headers[name] || headers[name.toLowerCase()] || ''
}

// A Nook that acts for the person making this request.
//
// The proxy puts their identity in the request's headers, and X-Nook-As is what lets this read
// and write their data. Pass the headers through rather than an email address: a nook can act for
// someone who is using it, not for anyone it can name.
function viewer(headers) {
  return new Nook(header(headers, 'X-Nook-As'), {
    email: header(headers, 'X-Nook-User'),
    name: header(headers, 'X-Nook-Name'),
    role: header(headers, 'X-Nook-Role'),
  })
}

// The nook acting as itself: shared collections only, for cron and anything with no viewer.
const self = new Nook()

module.exports = {
  Nook,
  NookError,
  viewer,
  list: self.list.bind(self),
  get: self.get.bind(self),
  create: self.create.bind(self),
  update: self.update.bind(self),
  delete: self.delete.bind(self),
}

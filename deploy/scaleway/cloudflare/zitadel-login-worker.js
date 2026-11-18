// Cloudflare Worker for auth.black-tulip.nl: injects Quill's skin
// (zitadel-login.css, inlined at deploy by build-zitadel-worker.sh) into
// Zitadel's hosted login pages. Everything else passes through untouched; if the
// Worker is removed, Zitadel simply shows its default look again.
//
// Route: auth.black-tulip.nl/ui/login/*
const CSS = "__ZITADEL_LOGIN_CSS__";

export default {
  async fetch(request) {
    const response = await fetch(request);
    const type = response.headers.get("content-type") || "";
    if (!type.includes("text/html")) return response;
    return new HTMLRewriter()
      .on("head", { element(el) { el.append(`<style id="quill-skin">${CSS}</style>`, { html: true }); } })
      .transform(response);
  },
};

// Cloudflare Worker for auth.black-tulip.nl: injects Quill's skin
// (zitadel-login.css, inlined at deploy by build-zitadel-worker.sh) into
// Zitadel's hosted login pages. Everything else passes through untouched; if the
// Worker is removed, Zitadel simply shows its default look again.
//
// Route: auth.black-tulip.nl/ui/login/*
const CSS = "__ZITADEL_LOGIN_CSS__";

const SIGN_UP_URL = "https://app.black-tulip.nl/sign-up";

export default {
  async fetch(request) {
    const response = await fetch(request);
    const type = response.headers.get("content-type") || "";
    if (!type.includes("text/html")) return response;

    // Zitadel's CSP only allows styles carrying its per-request nonce; an
    // un-nonced <style> is silently dropped by the browser.
    const csp = response.headers.get("content-security-policy") || "";
    const m = csp.match(/style-src[^;]*'nonce-([^']+)'/);
    const nonce = m ? ` nonce="${m[1]}"` : "";

    const path = new URL(request.url).pathname;
    const rewriter = new HTMLRewriter().on("head", {
      element(el) { el.append(`<style id="quill-skin"${nonce}>${CSS}</style>`, { html: true }); },
    });
    // Entry steps only: the login-name form, and the account picker Zitadel
    // shows instead when a browser session already exists.
    if (/\/(loginname|userselection|login)$/.test(path)) {
      rewriter.on(".content-container", {
        element(el) {
          el.append(
            `<p class="q-signup">New to Quill? <a href="${SIGN_UP_URL}">Create an account</a></p>`,
            { html: true },
          );
        },
      });
    }
    return rewriter.transform(response);
  },
};

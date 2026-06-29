// awst Containers — minimal Firefox Multi-Account Containers handler.
//
// Firefox invokes this page via the protocol_handler in manifest.json: opening
// `ext+awst-containers:name=…&url=…&color=…&icon=…` navigates here with the
// whole original URI as the `u` query param. We parse it, find-or-create the
// named container (contextual identity), open the target URL in it, then close
// this helper tab.
//
// Clean-room reimplementation of the same idea as the MIT-licensed
// common-fate/granted-containers extension; no upstream code is copied.

(async () => {
  const fail = (msg) => {
    document.body.textContent = "awst: " + msg;
  };

  const raw = new URL(location.href).searchParams.get("u") || "";
  const payload = raw.replace(/^ext\+awst-containers:/, "");
  const p = new URLSearchParams(payload);

  const name = p.get("name") || "aws";
  const url = p.get("url");
  const color = p.get("color") || "blue";
  const icon = p.get("icon") || "fingerprint";

  if (!url) {
    fail("missing url parameter");
    return;
  }

  try {
    let identity = (await browser.contextualIdentities.query({ name }))[0];
    if (!identity) {
      identity = await browser.contextualIdentities.create({ name, color, icon });
    } else if (identity.color !== color || identity.icon !== icon) {
      // Keep the per-profile color/icon stable if awst ever changes them.
      identity = await browser.contextualIdentities.update(identity.cookieStoreId, { color, icon });
    }

    await browser.tabs.create({ url, cookieStoreId: identity.cookieStoreId });

    const me = await browser.tabs.getCurrent();
    if (me) {
      await browser.tabs.remove(me.id);
    }
  } catch (e) {
    fail(String(e));
  }
})();

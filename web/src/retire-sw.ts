// multimux used to register a runtime-caching service worker (cache name
// "multimux-shell-v1"). It precached nothing and a live-terminal dashboard has
// nothing useful to serve offline, so it was removed rather than grown into a
// tested offline shell. Installability is unaffected: Chrome dropped the
// service-worker requirement for installing a web app in 108 (mobile) / 112
// (desktop), and multimux never used beforeinstallprompt — the manifest at
// /manifest.webmanifest is what makes each daemon installable.
//
// Workers already registered in a user's browser keep running after sw.js
// disappears from the build: the daemon's SPA fallback answers /sw.js with
// index.html, so the browser's update check fails and the stale worker stays
// alive, serving whatever it cached. Retire it explicitly on startup.
const legacyCache = "multimux-shell-v1";

/**
 * Unregisters any service worker left over from an older multimux build and
 * deletes its cache. Best effort and safe to call on every startup — once the
 * old worker is gone this is two no-op lookups.
 */
export async function retireServiceWorker(): Promise<void> {
  try {
    if ("serviceWorker" in navigator) {
      const registrations = await navigator.serviceWorker.getRegistrations();
      await Promise.all(registrations.map((r) => r.unregister()));
    }
    if ("caches" in globalThis) await caches.delete(legacyCache);
  } catch {
    // A browser that refuses either API just keeps the old worker; nothing the
    // page can do about it, and it must not break the app shell.
  }
}

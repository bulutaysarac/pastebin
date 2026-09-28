// applyTheme is defined inline in the page <head> so the theme is set before
// the first paint; this file only wires up the buttons.

// Theme toggle: flip light/dark and remember the choice.
document.getElementById("theme-toggle")?.addEventListener("click", () => {
  const next = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
  localStorage.setItem("theme", next);
  applyTheme(next);
});

// Copy buttons: <button data-copy="#selector"> copies that element's value
// (inputs) or text (everything else).
document.querySelectorAll("[data-copy]").forEach((btn) => {
  const label = btn.textContent;
  btn.addEventListener("click", async () => {
    const el = document.querySelector(btn.dataset.copy);
    await copyText(el instanceof HTMLInputElement ? el.value : el.textContent);
    btn.textContent = "Copied!";
    btn.classList.add("copied");
    setTimeout(() => {
      btn.textContent = label;
      btn.classList.remove("copied");
    }, 1500);
  });
});

// The Clipboard API only works on https:// and localhost. Anywhere else (for
// example a LAN IP) fall back to the old select-and-copy trick.
async function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.style.position = "fixed";
  area.style.opacity = "0";
  document.body.appendChild(area);
  area.select();
  document.execCommand("copy");
  area.remove();
}

// Syntax highlighting runs here in the browser, so the server's read path stays
// a plain database lookup. Very large pastes are left plain to keep the tab
// responsive.
const code = document.getElementById("paste-code");
if (code && window.hljs && code.textContent.length <= 200_000) {
  hljs.highlightElement(code);
}

// Ctrl/Cmd + Enter submits the new-paste form.
document.querySelector(".editor")?.addEventListener("keydown", (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
    event.target.form.requestSubmit();
  }
});

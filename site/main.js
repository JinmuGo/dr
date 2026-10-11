// dr.jinmugo.com: install tabs, copy buttons and dr-style keys. The page works without it.
(() => {
  const mode = document.getElementById("mode");
  const announce = document.getElementById("announce");
  const tabs = [...document.querySelectorAll('[role="tab"]')];
  const panels = tabs.map((tab) => document.getElementById(tab.getAttribute("aria-controls")));

  const select = (index, focus) => {
    tabs.forEach((tab, i) => {
      const on = i === index;
      tab.setAttribute("aria-selected", String(on));
      tab.tabIndex = on ? 0 : -1;
      panels[i].hidden = !on;
    });
    if (focus) tabs[index].focus();
  };

  tabs.forEach((tab, i) => {
    tab.addEventListener("click", () => select(i));
    tab.addEventListener("keydown", (event) => {
      if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
      event.preventDefault();
      const step = event.key === "ArrowRight" ? 1 : tabs.length - 1;
      select((i + step) % tabs.length, true);
    });
  });
  select(0);

  // Copying shows in the status line the way vim names a yank.
  let modeTimer;
  const flash = (text) => {
    mode.textContent = text;
    mode.classList.add("yanked");
    clearTimeout(modeTimer);
    modeTimer = setTimeout(() => {
      mode.textContent = "NORMAL";
      mode.classList.remove("yanked");
    }, 1200);
  };

  const copy = async (button) => {
    const code = button.parentElement.querySelector("code");
    try {
      await navigator.clipboard.writeText(code.textContent);
    } catch {
      // Without clipboard access, select the command so it can be copied by hand.
      const range = document.createRange();
      range.selectNodeContents(code);
      const selection = getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      return;
    }
    button.textContent = "copied";
    button.classList.add("done");
    setTimeout(() => {
      button.textContent = "copy";
      button.classList.remove("done");
    }, 1200);
    flash("YANKED");
    announce.textContent = `Copied: ${code.textContent}`;
  };

  document.querySelectorAll(".copy").forEach((button) => {
    button.addEventListener("click", () => copy(button));
  });

  document.addEventListener("keydown", (event) => {
    if (event.metaKey || event.ctrlKey || event.altKey || event.isComposing) return;
    if (event.target.closest("input, textarea, select, [contenteditable]")) return;
    if (event.key >= "1" && event.key <= String(tabs.length)) {
      select(Number(event.key) - 1);
    } else if (event.key === "i") {
      const install = document.getElementById("install");
      install.scrollIntoView();
      install.focus({ preventScroll: true });
    } else if (event.key === "y") {
      copy(panels.find((panel) => !panel.hidden).querySelector(".copy"));
    } else if (event.key === "g") {
      location.href = "https://github.com/JinmuGo/dr";
    }
  });

  // Rows and cards come in as they scroll into view, like a diff being drawn.
  if (!("IntersectionObserver" in window) || matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const items = document.querySelectorAll(".row, .card");
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (!entry.isIntersecting) continue;
        entry.target.classList.add("in");
        observer.unobserve(entry.target);
      }
    },
    { threshold: 0.2 },
  );
  items.forEach((item, i) => {
    item.classList.add("reveal");
    item.style.setProperty("--i", String(i % 5));
    observer.observe(item);
  });
})();

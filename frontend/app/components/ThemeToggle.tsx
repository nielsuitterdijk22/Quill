"use client";

import { useEffect, useState } from "react";

const KEY = "quill-theme";

export function ThemeToggle() {
  const [dark, setDark] = useState(true);

  useEffect(() => {
    const saved = localStorage.getItem(KEY);
    const isDark = saved !== "light";
    setDark(isDark);
    document.documentElement.dataset.theme = isDark ? "" : "light";
    if (!isDark) document.documentElement.setAttribute("data-theme", "light");
    else document.documentElement.removeAttribute("data-theme");
  }, []);

  function toggle() {
    const next = !dark;
    setDark(next);
    localStorage.setItem(KEY, next ? "dark" : "light");
    if (!next) document.documentElement.setAttribute("data-theme", "light");
    else document.documentElement.removeAttribute("data-theme");
  }

  return (
    <button type="button" className="theme-toggle" onClick={toggle} title="Toggle light/dark mode">
      {dark ? "☀" : "◑"} {dark ? "Light mode" : "Dark mode"}
    </button>
  );
}

"use strict";
// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
const filter = document.querySelector("[data-filter]");
if (filter) {
  const entries = [...document.querySelectorAll("[data-search]")];
  const status = document.querySelector("[data-count]");
  const update = () => {
    const query = filter.value.trim().toLocaleLowerCase();
    let count = 0;
    for (const entry of entries) {
      entry.hidden = !entry.textContent.toLocaleLowerCase().includes(query);
      if (!entry.hidden) count += 1;
    }
    for (const section of document.querySelectorAll(".namespace")) {
      section.hidden = ![...section.querySelectorAll("[data-search]")].some((entry) => !entry.hidden);
      if (section.tagName === "DETAILS") section.open = Boolean(query) && !section.hidden;
    }
    status.textContent = `${entries.length} 件中 ${count} 件を表示`;
  };
  filter.addEventListener("input", update);
  update();
}

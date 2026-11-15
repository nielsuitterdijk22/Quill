"use client";

import { useMemo, useState } from "react";

import type { DiffFile } from "../../../../../../../lib/api";

type Node = {
  name: string; // display name (single-child folders collapse: "a/b/c")
  path: string;
  children: Node[];
  file?: { index: number; f: DiffFile };
};

// buildTree turns flat paths into folders-first, alphabetical nodes, folding
// chains of single-folder directories into one row (as GitHub does).
function buildTree(files: DiffFile[]): Node[] {
  const root: Node = { name: "", path: "", children: [] };
  files.forEach((f, index) => {
    const parts = f.path.split("/");
    let cur = root;
    parts.forEach((part, i) => {
      const path = parts.slice(0, i + 1).join("/");
      let next = cur.children.find((c) => c.name === part && !c.file === (i < parts.length - 1));
      if (!next) {
        next = { name: part, path, children: [] };
        if (i === parts.length - 1) next.file = { index, f };
        cur.children.push(next);
      }
      cur = next;
    });
  });
  const fold = (n: Node): Node => {
    let node = n;
    while (!node.file && node.children.length === 1 && !node.children[0].file) {
      const only = node.children[0];
      node = { ...only, name: `${node.name}/${only.name}` };
    }
    const children = node.children.map(fold).sort((a, b) => {
      if (!a.file !== !b.file) return a.file ? 1 : -1;
      return a.name.localeCompare(b.name);
    });
    return { ...node, children };
  };
  return root.children.map(fold).sort((a, b) => {
    if (!a.file !== !b.file) return a.file ? 1 : -1;
    return a.name.localeCompare(b.name);
  });
}

const STATUS_CLASS: Record<string, string> = {
  added: "added",
  deleted: "deleted",
  renamed: "renamed",
};

// DiffFileTree is the Files-changed navigator: every changed file in its
// folder, with +/- counts and review-comment counts; click jumps to the file.
export function DiffFileTree({
  files,
  activeIndex,
  commentCounts,
  onSelect,
}: {
  files: DiffFile[];
  activeIndex: number;
  commentCounts: Map<string, number>;
  onSelect: (index: number) => void;
}) {
  const [query, setQuery] = useState("");
  const [closed, setClosed] = useState<Set<string>>(new Set());
  const q = query.trim().toLowerCase();
  const shown = useMemo(
    () => (q ? files.filter((f) => f.path.toLowerCase().includes(q)) : files),
    [files, q],
  );
  const tree = useMemo(() => buildTree(shown), [shown]);

  const toggle = (path: string) =>
    setClosed((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });

  const render = (nodes: Node[], depth: number) =>
    nodes.map((n) => {
      const pad = { paddingLeft: 8 + depth * 14 };
      if (n.file) {
        const { index, f } = n.file;
        const comments = commentCounts.get(f.path) ?? 0;
        return (
          <button
            key={n.path}
            type="button"
            className={`dft-row file${index === activeIndex ? " active" : ""}`}
            style={pad}
            title={f.path}
            onClick={() => onSelect(index)}
          >
            <span className={`dft-dot ${STATUS_CLASS[f.status] ?? "modified"}`} />
            <span className="dft-name">{n.name}</span>
            {comments > 0 && <span className="dft-comments">{comments}</span>}
            <span className="dft-stat">
              {f.additions > 0 && <span className="add">+{f.additions}</span>}
              {f.deletions > 0 && <span className="del">−{f.deletions}</span>}
            </span>
          </button>
        );
      }
      const isClosed = closed.has(n.path) && !q;
      return (
        <div key={n.path}>
          <button
            type="button"
            className="dft-row dir"
            style={pad}
            aria-expanded={!isClosed}
            onClick={() => toggle(n.path)}
          >
            <span className="dft-caret">{isClosed ? "▸" : "▾"}</span>
            <span className="dft-name">{n.name}</span>
          </button>
          {!isClosed && render(n.children, depth + 1)}
        </div>
      );
    });

  return (
    <nav className="diff-tree" aria-label="Changed files">
      <div className="dft-head">
        <span>
          {files.length} file{files.length === 1 ? "" : "s"}
        </span>
      </div>
      <input
        className="dft-filter"
        type="search"
        placeholder="Filter files…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        aria-label="Filter changed files"
      />
      <div className="dft-list">
        {tree.length ? render(tree, 0) : <div className="dft-empty">No matching files</div>}
      </div>
    </nav>
  );
}

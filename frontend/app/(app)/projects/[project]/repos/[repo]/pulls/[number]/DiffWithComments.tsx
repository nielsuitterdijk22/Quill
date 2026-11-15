"use client";

import { Fragment, useEffect, useRef, useState } from "react";
import { useFormState, useFormStatus } from "react-dom";

import type { DiffFile, LineComment } from "../../../../../../../lib/api";
import { addLineCommentAction, type LineCommentState } from "./actions";
import { DiffFileTree } from "./DiffFileTree";

const STATUS_GLYPH: Record<string, string> = {
  added: "A",
  deleted: "D",
  renamed: "R",
  modified: "M",
};

// lineKey identifies a commentable line by file path + new-file line number.
function lineKey(path: string, line: number): string {
  return `${path}\u0000${line}`;
}

// hunkHeading keeps the @@ range marker but trims trailing context.
function hunkHeading(header: string): string {
  const end = header.lastIndexOf("@@");
  return end > 0 ? header.slice(0, end + 2) : header;
}

function fmtDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
}

const initialState: LineCommentState = {};

function SubmitButton() {
  const { pending } = useFormStatus();
  return (
    <button className="btn primary" type="submit" disabled={pending}>
      {pending ? "Commenting…" : "Add comment"}
    </button>
  );
}

// LineCommentForm posts one comment anchored to a diff line. On success the page
// revalidates server-side and onDone closes the inline editor.
function LineCommentForm({
  project,
  repo,
  number,
  path,
  line,
  onDone,
}: {
  project: string;
  repo: string;
  number: number;
  path: string;
  line: number;
  onDone: () => void;
}) {
  const action = addLineCommentAction.bind(null, project, repo, number, path, line);
  const [state, formAction] = useFormState(action, initialState);
  const ref = useRef<HTMLFormElement>(null);

  useEffect(() => {
    if (state.ok) {
      ref.current?.reset();
      onDone();
    }
  }, [state, onDone]);

  return (
    <form className="line-comment-form" action={formAction} ref={ref}>
      {state.error && <div className="form-error">{state.error}</div>}
      <textarea
        name="body"
        rows={3}
        aria-label={`Comment on line ${line}`}
        placeholder="Leave a comment on this line"
        autoFocus
      />
      <div className="form-actions">
        <button type="button" className="btn ghost" onClick={onDone}>
          Cancel
        </button>
        <SubmitButton />
      </div>
    </form>
  );
}

function LineCommentView({ c }: { c: LineComment }) {
  return (
    <div className="line-comment">
      <div className="line-comment-head">
        <b>{c.author ?? "unknown"}</b>
        <span className="subtle"> · {fmtDate(c.createdAt)}</span>
      </div>
      <div className="line-comment-body">
        <pre>{c.body}</pre>
      </div>
    </div>
  );
}

// DiffWithComments renders a PR's unified diff and lets the reader click any line
// in the new file to leave a review comment. Existing line comments are shown
// inline beneath the line they anchor to (matched by path + new line number).
export function DiffWithComments({
  project,
  repo,
  number,
  files,
  comments,
}: {
  project: string;
  repo: string;
  number: number;
  files: DiffFile[];
  comments: LineComment[];
}) {
  const [active, setActive] = useState<string | null>(null);
  const [current, setCurrent] = useState(0);
  const [collapsed, setCollapsed] = useState<Set<number>>(new Set());

  // Track which file is in view so the tree highlights it while scrolling.
  useEffect(() => {
    const els = files.map((_, i) => document.getElementById(`diff-file-${i}`));
    const io = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible[0]) setCurrent(Number(visible[0].target.getAttribute("data-index")));
      },
      { rootMargin: "-10% 0px -70% 0px" },
    );
    els.forEach((el) => el && io.observe(el));
    return () => io.disconnect();
  }, [files]);

  const jump = (i: number) => {
    setCollapsed((prev) => {
      if (!prev.has(i)) return prev;
      const next = new Set(prev);
      next.delete(i);
      return next;
    });
    setCurrent(i);
    document.getElementById(`diff-file-${i}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
  };
  const toggleFile = (i: number) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });

  if (files.length === 0) {
    return <div className="empty">No changes to display.</div>;
  }

  const byLine = new Map<string, LineComment[]>();
  for (const c of comments) {
    const k = lineKey(c.path, c.line);
    const arr = byLine.get(k);
    if (arr) arr.push(c);
    else byLine.set(k, [c]);
  }
  const commentCounts = new Map<string, number>();
  for (const c of comments) commentCounts.set(c.path, (commentCounts.get(c.path) ?? 0) + 1);

  return (
    <div className="diff-layout">
      <DiffFileTree
        files={files}
        activeIndex={current}
        commentCounts={commentCounts}
        onSelect={jump}
      />
    <div className="diff">
      {files.map((f, fi) => (
        <div className="diff-file" key={f.path} id={`diff-file-${fi}`} data-index={fi}>
          <div className="diff-file-head">
            <button
              type="button"
              className="diff-collapse"
              aria-label={collapsed.has(fi) ? "Expand file" : "Collapse file"}
              aria-expanded={!collapsed.has(fi)}
              onClick={() => toggleFile(fi)}
            >
              {collapsed.has(fi) ? "▸" : "▾"}
            </button>
            <span className={`diff-status ${f.status}`}>
              {STATUS_GLYPH[f.status] ?? "M"}
            </span>
            <span className="mono path">
              {f.status === "renamed" ? `${f.oldPath} → ${f.path}` : f.path}
            </span>
            <span className="spacer" />
            <span className="diff-file-stat">
              {f.additions > 0 && <span className="add">+{f.additions}</span>}
              {f.deletions > 0 && <span className="del">−{f.deletions}</span>}
            </span>
          </div>
          {collapsed.has(fi) ? null : f.isBinary ? (
            <div className="diff-binary">Binary file not shown.</div>
          ) : (
            <table className="diff-table">
              <tbody>
                {f.hunks.map((h, hi) => (
                  <Fragment key={hi}>
                    <tr className="hunk">
                      <td className="ln" />
                      <td className="ln" />
                      <td className="code">{hunkHeading(h.header)}</td>
                    </tr>
                    {h.lines.map((l, i) => {
                      const commentable = l.newNumber > 0;
                      const k = lineKey(f.path, l.newNumber);
                      const lineComments = commentable
                        ? (byLine.get(k) ?? [])
                        : [];
                      const isActive = active === k;
                      return (
                        <Fragment key={i}>
                          <tr
                            className={`dl ${l.type}${
                              commentable ? " commentable" : ""
                            }`}
                          >
                            <td className="ln">{l.oldNumber || ""}</td>
                            <td className="ln gutter-new">
                              {commentable && (
                                <button
                                  type="button"
                                  className="add-comment-btn"
                                  aria-label={`Comment on line ${l.newNumber}`}
                                  onClick={() => setActive(isActive ? null : k)}
                                >
                                  +
                                </button>
                              )}
                              <span className="ln-num">
                                {l.newNumber || ""}
                              </span>
                            </td>
                            <td className="code">
                              <span className="sign">
                                {l.type === "add"
                                  ? "+"
                                  : l.type === "del"
                                    ? "−"
                                    : " "}
                              </span>
                              {l.content}
                            </td>
                          </tr>
                          {lineComments.map((c) => (
                            <tr className="line-comment-row" key={c.id}>
                              <td className="ln" />
                              <td className="ln" />
                              <td className="code">
                                <LineCommentView c={c} />
                              </td>
                            </tr>
                          ))}
                          {isActive && (
                            <tr className="line-comment-row">
                              <td className="ln" />
                              <td className="ln" />
                              <td className="code">
                                <LineCommentForm
                                  project={project}
                                  repo={repo}
                                  number={number}
                                  path={f.path}
                                  line={l.newNumber}
                                  onDone={() => setActive(null)}
                                />
                              </td>
                            </tr>
                          )}
                        </Fragment>
                      );
                    })}
                  </Fragment>
                ))}
              </tbody>
            </table>
          )}
        </div>
      ))}
    </div>
    </div>
  );
}

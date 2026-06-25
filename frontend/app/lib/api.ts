// Server-side API client for the Quill backend.
//
// Browser code should call the rewrite at /api/backend/* (see next.config.mjs);
// server components use QUILL_API_BASE_URL directly. Imports from this file
// continue to work unchanged; the implementation lives in ./api/* by domain.

export * from "./api/types";
export * from "./api/meta";
export * from "./api/auth";
export * from "./api/projects";
export * from "./api/repos";
export * from "./api/pulls";
export * from "./api/pipelines";
export * from "./api/issues";
export * from "./api/policies";
export * from "./api/admin";
export * from "./api/audit";

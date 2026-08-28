/**
 * Minimal local type definitions for the WebMCP `navigator.modelContext` /
 * `document.modelContext` surface (no published TypeScript types exist yet
 * for this draft W3C Web Machine Learning CG API). Only the subset this app
 * uses is declared. Both entry points are declared because real hosts have
 * been observed exposing either: Chrome exposes both as the same object,
 * but some hosts may only inject the document-scoped one.
 */

export interface WebMCPContentItem {
  type: 'text';
  text: string;
}

export interface WebMCPToolResult {
  content: WebMCPContentItem[];
  isError?: boolean;
}

export interface WebMCPToolDescriptor {
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
  execute(args: Record<string, unknown>): Promise<WebMCPToolResult> | WebMCPToolResult;
}

export interface WebMCPToolHandle {
  unregister(): void;
}

export interface ModelContext {
  registerTool(tool: WebMCPToolDescriptor): WebMCPToolHandle | void;
}

declare global {
  interface Navigator {
    modelContext?: ModelContext;
  }
  interface Document {
    modelContext?: ModelContext;
  }
}

export type Session = {
  id: number;
  tmuxName: string;
  toolId: number;
  dir: string;
  status: string;
  repoUrl?: string;
  branch?: string;
  gitState?: "untracked" | "modified" | "clean";
  ahead?: number;
  behind?: number;
  noUpstream?: boolean;
};
export type Tool = { id: number; name: string; command: string };
export type Dir = { id: number; name: string; path: string };

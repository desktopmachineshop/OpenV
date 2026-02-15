import { create } from 'zustand';
import { Artifact, Link, Project } from '../api/client';

interface AppState {
  projectId: string;
  setProjectId: (id: string) => void;
  projects: Project[];
  setProjects: (projects: Project[]) => void;
  addProject: (project: Project) => void;
  updateProject: (project: Project) => void;
  removeProject: (id: string) => void;
  artifacts: Artifact[];
  setArtifacts: (artifacts: Artifact[]) => void;
  addArtifact: (artifact: Artifact) => void;
  updateArtifact: (artifact: Artifact) => void;
  removeArtifact: (id: string) => void;
  links: Link[];
  setLinks: (links: Link[]) => void;
  addLink: (link: Link) => void;
  updateLink: (link: Link) => void;
  removeLink: (id: string) => void;
  selectedArtifactId: string | null;
  setSelectedArtifactId: (id: string | null) => void;
}

export const useAppStore = create<AppState>((set) => ({
  projectId: '',
  setProjectId: (id: string) => set({ projectId: id }),
  
  projects: [],
  setProjects: (projects: Project[]) => set({ projects: projects || [] }),
  addProject: (project: Project) =>
    set((state) => ({ projects: [...state.projects, project] })),
  updateProject: (project: Project) =>
    set((state) => ({
      projects: state.projects.map((p) =>
        p.id === project.id ? project : p
      ),
    })),
  removeProject: (id: string) =>
    set((state) => ({
      projects: state.projects.filter((p) => p.id !== id),
    })),
  
  artifacts: [],
  setArtifacts: (artifacts: Artifact[]) => set({ artifacts: artifacts || [] }),
  addArtifact: (artifact: Artifact) =>
    set((state) => ({ artifacts: [...state.artifacts, artifact] })),
  updateArtifact: (artifact: Artifact) =>
    set((state) => ({
      artifacts: state.artifacts.map((a) =>
        a.id === artifact.id ? artifact : a
      ),
    })),
  removeArtifact: (id: string) =>
    set((state) => ({
      artifacts: state.artifacts.filter((a) => a.id !== id),
    })),
  
  links: [],
  setLinks: (links: Link[]) => set({ links: links || [] }),
  addLink: (link: Link) =>
    set((state) => ({ links: [...state.links, link] })),
  updateLink: (link: Link) =>
    set((state) => ({
      links: state.links.map((l) =>
        l.id === link.id ? link : l
      ),
    })),
  removeLink: (id: string) =>
    set((state) => ({
      links: state.links.filter((l) => l.id !== id),
    })),
  
  selectedArtifactId: null,
  setSelectedArtifactId: (id: string | null) =>
    set({ selectedArtifactId: id }),
}));

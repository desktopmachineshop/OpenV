import { create } from 'zustand';
import { Artifact, ArtifactTypeDef, Link, LinkTypeRule, Org, OrgFeatures, Project, User } from '../api/client';
import { writeActiveOrg } from './activeOrgStorage';

interface MetaState {
  artifactTypes: ArtifactTypeDef[];
  linkTypeRules: LinkTypeRule[];
  loaded: boolean;
}

interface AppState {
  currentUser: User | null;
  setCurrentUser: (user: User | null) => void;
  // Whether this server walls unverified accounts (from /auth/config).
  // Defaults false so an unreachable config never walls anyone client-side;
  // the API's own gate still protects data.
  emailVerificationRequired: boolean;
  setEmailVerificationRequired: (required: boolean) => void;
  meta: MetaState;
  setMeta: (meta: Partial<MetaState>) => void;
  orgs: Org[];
  setOrgs: (orgs: Org[]) => void;
  activeOrgId: string;
  /**
   * Set the active workspace. Persists to session+local storage and clears the
   * loaded project list (a workspace switch invalidates it). Pass
   * `{ clearProjects: false }` for the initial boot assignment.
   */
  setActiveOrgId: (id: string, opts?: { clearProjects?: boolean }) => void;
  orgsLoaded: boolean;
  setOrgsLoaded: (loaded: boolean) => void;
  // The caller's feature gates in the active workspace (REQ-137); null
  // until loaded, and reloaded on every workspace switch. useFeature reads
  // it and answers false while it is null, so a gated UI never flashes on.
  features: OrgFeatures | null;
  setFeatures: (features: OrgFeatures | null) => void;
  // Whether the gate load failed with no timed retry left (#379 bug 179),
  // which leaves features null as a load still on its way does: a page that
  // waits for the gates takes a failure as its feature off (#379 bug 174).
  // setFeatures clears it.
  featuresFailed: boolean;
  setFeaturesFailed: () => void;
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
  currentUser: null,
  setCurrentUser: (user: User | null) => set({ currentUser: user }),
  emailVerificationRequired: false,
  setEmailVerificationRequired: (required: boolean) => set({ emailVerificationRequired: required }),

  meta: { artifactTypes: [], linkTypeRules: [], loaded: false },
  setMeta: (meta: Partial<MetaState>) =>
    set((state) => ({ meta: { ...state.meta, ...meta } })),

  orgs: [],
  setOrgs: (orgs: Org[]) => set({ orgs: orgs || [] }),
  features: null,
  setFeatures: (features: OrgFeatures | null) => set({ features, featuresFailed: false }),
  featuresFailed: false,
  setFeaturesFailed: () => set({ featuresFailed: true }),
  activeOrgId: '',
  setActiveOrgId: (id: string, opts?: { clearProjects?: boolean }) => {
    writeActiveOrg(id);
    const clearProjects = opts?.clearProjects !== false;
    set((state) => ({
      activeOrgId: id,
      projects: clearProjects ? [] : state.projects,
    }));
  },
  orgsLoaded: false,
  setOrgsLoaded: (loaded: boolean) => set({ orgsLoaded: loaded }),

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

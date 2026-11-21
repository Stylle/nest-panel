export type Site = {
  id: string;
  title: string;
  url: string;
  description: string;
  icon: string;
};
export type Group = { id: string; title: string; sites: Site[] };
export type Theme = {
  palette: string;
  layout: string;
  icons: string;
  density: string;
  background: string;
  opacity: number;
  blur: number;
};
export type Config = {
  revision: number;
  name: string;
  public: boolean;
  theme: Theme;
  groups: Group[];
};
export type Candidate = {
  id: string;
  name: string;
  url: string;
  state: string;
  existing: boolean;
};

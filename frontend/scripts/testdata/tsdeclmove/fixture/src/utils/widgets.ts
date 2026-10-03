export interface Widget {
  name: string;
}

export const makeWidget = (name: string): Widget => ({ name });

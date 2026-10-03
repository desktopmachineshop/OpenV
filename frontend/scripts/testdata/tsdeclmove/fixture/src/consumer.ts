import client, { base, itemAPI, METHODS, type Account, type Item } from './api/client';

export const first = (items: Item[]): Item | undefined => items[0];
export const owner = (account: Account): string => account.id;
export const all = () => itemAPI.list();
export const origin = (): string => `${base()}${String(client.defaults.baseURL)}${METHODS.join()}`;

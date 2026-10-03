// Accounts.

import { client } from './http';
import type { Account } from './types/accounts';

export const accountAPI = { me: () => client.get<Account>('/api/me') }; // one line

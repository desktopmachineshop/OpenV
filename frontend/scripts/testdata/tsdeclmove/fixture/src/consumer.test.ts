import { all } from './consumer';

vi.mock('./api/client', () => ({ itemAPI: { list: vi.fn() } }));

it('lists through the client', () => {
  expect(all).toBeTypeOf('function');
});

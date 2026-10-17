import { initActivityPage } from '../../src/pages/activity';
import type { Socket } from '../../src/socket';
const root = document.getElementById('page-activity')!;
root.classList.add('active');
root.style.display = 'block';
initActivityPage({ on: () => {} } as unknown as Socket, () => true,
  () => [{ id: 'r1', label: 'Synthetic Router' }, { id: 'r2', label: 'Second synthetic router' }], () => 'r1');

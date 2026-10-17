import { initConfigManagementPage } from '../../src/pages/config-management';
import type { Socket } from '../../src/socket';
const root = document.getElementById('page-config-management')!;
root.classList.add('active');
root.style.display = 'block';
const socket = { on: () => {}, emit: () => {} } as unknown as Socket;
initConfigManagementPage(socket, (page) => page === 'config-management');
document.dispatchEvent(new CustomEvent('mikrodash:pagechange', { detail: 'config-management' }));

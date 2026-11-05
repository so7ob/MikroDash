import { initSubscribersPage } from '../../src/pages/subscribers';
initSubscribersPage();
document.dispatchEvent(new CustomEvent('mikrodash:pagechange', {detail:'subscribers'}));

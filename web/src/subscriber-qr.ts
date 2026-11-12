import { voucherCode } from './subscriber-login';

type QRDecoder = (pixels: Uint8ClampedArray, width: number, height: number, options: { inversionAttempts: string }) => { data: string } | null;
declare global { interface Window { jsQR?: QRDecoder } }

// Local decoding only. Closing, hiding or leaving the page releases the camera,
// including permission requests that resolve after the dialog was closed.
export function initSubscriberQR(apply: (code: string) => void): { close: () => void } {
  const dialog = document.getElementById('portalQRDialog') as HTMLDialogElement;
  const video = document.getElementById('portalQRVideo') as HTMLVideoElement;
  const file = document.getElementById('portalQRFile') as HTMLInputElement;
  const camera = document.getElementById('portalQRCamera') as HTMLButtonElement;
  const opener = document.getElementById('portalQR') as HTMLButtonElement;
  const feedback = document.getElementById('portalQRFeedback')!;
  let stream: MediaStream | null = null, timer: ReturnType<typeof setTimeout> | undefined, epoch = 0;
  const say = (text: string) => { feedback.textContent = text; };
  function stop(): void {
    epoch++; clearTimeout(timer); stream?.getTracks().forEach(track => track.stop()); stream = null;
    video.pause(); video.srcObject = null; video.hidden = true; camera.disabled = false; file.value = '';
  }
  function close(): void { stop(); if (dialog.open) dialog.close(); }
  function decode(source: CanvasImageSource, width: number, height: number): string {
    if (!window.jsQR) throw new Error('QR unavailable');
    if (!width || !height) return '';
    if (width * height > 20000000) throw new Error('Image too large');
    const scale = Math.min(1, 1600 / Math.max(width, height));
    const canvas = document.createElement('canvas'); canvas.width = Math.round(width * scale); canvas.height = Math.round(height * scale);
    const ctx = canvas.getContext('2d', { willReadFrequently: true }); if (!ctx) throw new Error('Canvas unavailable');
    ctx.drawImage(source, 0, 0, canvas.width, canvas.height);
    return window.jsQR(ctx.getImageData(0, 0, canvas.width, canvas.height).data, canvas.width, canvas.height, { inversionAttempts: 'attemptBoth' })?.data || '';
  }
  function accept(raw: string): boolean {
    if (!raw) return false;
    const code = voucherCode(raw);
    if (!code) { say('الرمز لا يحتوي على قسيمة صالحة. أدخل الرمز يدوياً.'); return false; }
    close(); apply(code); return true;
  }
  opener.addEventListener('click', () => { stop(); say('اختر صورة QR أو افتح الكاميرا. يمكنك دائماً إدخال الرمز يدوياً.'); dialog.showModal(); });
  document.getElementById('portalQRClose')!.addEventListener('click', close);
  dialog.addEventListener('keydown', event => {
    if (event.key !== 'Tab') return;
    const controls = Array.from(dialog.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled)')).filter(control => !control.hidden);
    const first = controls[0], last = controls.at(-1);
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
  });
  dialog.addEventListener('cancel', stop);
  dialog.addEventListener('close', stop);
  document.addEventListener('visibilitychange', () => { if (document.hidden) close(); });
  window.addEventListener('pagehide', close);
  file.addEventListener('change', async () => {
    const image = file.files?.[0]; if (!image) return;
    stop(); const expected = epoch;
    if (image.size > 5 * 1024 * 1024) { say('اختر صورة أصغر من 5 ميجابايت.'); return; }
    say('جارٍ قراءة الرمز من الصورة…');
    let bitmap: ImageBitmap | undefined;
    try {
      bitmap = await createImageBitmap(image);
      if (expected !== epoch || !dialog.open) return;
      const raw = decode(bitmap, bitmap.width, bitmap.height);
      if (!raw) say('لم يُعثر على رمز QR. جرّب صورة أوضح أو أدخل القسيمة يدوياً.'); else accept(raw);
    } catch { if (expected === epoch) say('تعذرت قراءة الصورة. جرّب صورة أخرى أو أدخل القسيمة يدوياً.'); }
    finally { bitmap?.close(); }
  });
  camera.addEventListener('click', async () => {
    stop(); const expected = epoch; camera.disabled = true;
    if (!navigator.mediaDevices?.getUserMedia || !window.isSecureContext) { camera.disabled = false; say('الكاميرا غير متاحة. اختر صورة QR أو أدخل الرمز يدوياً.'); return; }
    try {
      const next = await navigator.mediaDevices.getUserMedia({ audio: false, video: { facingMode: { ideal: 'environment' } } });
      if (expected !== epoch || !dialog.open) { next.getTracks().forEach(track => track.stop()); return; }
      stream = next; video.srcObject = next; video.hidden = false; await video.play();
      if (expected !== epoch) return;
      say('وجّه الكاميرا إلى رمز القسيمة.');
      const scan = () => {
        if (expected !== epoch || !stream || !dialog.open) return;
        try { if (video.readyState >= 2 && accept(decode(video, video.videoWidth, video.videoHeight))) return; }
        catch { stop(); say('تعذرت قراءة الكاميرا. اختر صورة أو أدخل الرمز يدوياً.'); return; }
        timer = setTimeout(scan, 280);
      };
      scan();
    } catch { if (expected === epoch) { stop(); say('تعذر فتح الكاميرا. اسمح بالوصول أو اختر صورة QR.'); } }
  });
  return { close };
}

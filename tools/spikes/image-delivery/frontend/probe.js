const endpoint = '/__cover_spike/image/1';
const cover = document.querySelector('#cover');
const placeholder = document.querySelector('#placeholder');
const output = document.querySelector('#result');
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));

async function display(state) {
  await window.go.main.Probe.Select(state);
  const response = await fetch(endpoint); // Same URL; rely on server no-store, not a cache-buster.
  if (!response.ok) { cover.hidden = true; placeholder.hidden = false; return {ok:false, response}; }
  const objectURL = URL.createObjectURL(await response.blob());
  try {
    cover.src = objectURL;
    await cover.decode();
    cover.hidden = false; placeholder.hidden = true;
    const canvas = document.createElement('canvas'); canvas.width = canvas.height = 1;
    const context = canvas.getContext('2d'); context.drawImage(cover, 0, 0, 1, 1);
    return {ok:true, pixel:[...context.getImageData(0,0,1,1).data], response};
  } catch {
    cover.hidden = true; placeholder.hidden = false;
    return {ok:false, response};
  } finally { URL.revokeObjectURL(objectURL); }
}

async function run() {
  for (let count=0; !window.go?.main?.Probe; count++) {
    if (count >= 100) throw new Error('Go APIが準備できませんでした。');
    await delay(100);
  }
  const red = await display('red');
  const blue = await display('blue');
  const missing = await display('missing');
  const broken = await display('broken');
  const unset = await display('unset');
  const result = {
    initial:red.ok && red.pixel[0]===255 && red.pixel[2]===0,
    replacement:blue.ok && blue.pixel[0]===0 && blue.pixel[2]===255,
    missing:!missing.ok && missing.response.status===404,
    broken:!broken.ok && broken.response.status===200,
    unset:!unset.ok && unset.response.status===404,
    methodRejected:(await fetch(endpoint,{method:'POST'})).status===405,
    encodedRejected:(await fetch('/__cover_spike/image/%2531')).status===404,
    unknownRejected:(await fetch('/__cover_spike/image/999')).status===404,
    noStore:red.response.headers.get('cache-control')==='no-store' && blue.response.headers.get('cache-control')==='no-store',
  };
  await window.go.main.Probe.Report(result);
  output.textContent = JSON.stringify(result,null,2);
  await display('blue');
  for (const button of document.querySelectorAll('button')) {
    button.addEventListener('click', async () => {
      for (const item of document.querySelectorAll('button')) item.disabled = true;
      try { await display(button.dataset.state); }
      catch { output.textContent = '仮画像の切り替えに失敗しました。'; }
      finally { for (const item of document.querySelectorAll('button')) item.disabled = false; }
    });
  }
}
run().catch(() => { output.textContent = '検証に失敗しました。Go APIと配信経路を確認してください。'; });

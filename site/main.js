// Commands remain visible and selectable without clipboard access or JavaScript.
const status = document.querySelector('.copy-status');
let statusTimer;

document.querySelectorAll('.copy').forEach((button) => {
  button.hidden = false;
  let buttonTimer;
  button.addEventListener('click', async () => {
    const command = button.parentElement.querySelector('code');
    clearTimeout(statusTimer);
    clearTimeout(buttonTimer);
    try {
      await navigator.clipboard.writeText(command.textContent);
      button.textContent = 'Copied';
      button.setAttribute('data-copied', '');
      status.textContent = 'Command copied to clipboard.';
    } catch {
      const range = document.createRange();
      range.selectNodeContents(command);
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      button.textContent = 'Copy';
      button.removeAttribute('data-copied');
      status.textContent = 'Copy unavailable. Command selected; copy it manually.';
    }
    buttonTimer = setTimeout(() => {
      button.textContent = 'Copy';
      button.removeAttribute('data-copied');
    }, 2200);
    statusTimer = setTimeout(() => { status.textContent = ''; }, 5000);
  });
});

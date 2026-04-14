(function() {
  if (!('IntersectionObserver' in window)) {
    document.querySelectorAll('.reveal').forEach(function(el) {
      el.classList.add('visible');
    });
    return;
  }

  var observer = new IntersectionObserver(function(entries) {
    entries.forEach(function(entry) {
      if (entry.isIntersecting) {
        entry.target.classList.add('visible');
        observer.unobserve(entry.target);
      }
    });
  }, {
    threshold: 0.12,
    rootMargin: '0px 0px -40px 0px'
  });

  document.querySelectorAll('.reveal').forEach(function(el) {
    observer.observe(el);
  });
})();

/* Auto-detect OS and update hero download button */
(function() {
  var btn = document.querySelector('.hero .btn-primary');
  if (!btn) return;

  var ua = navigator.userAgent || '';
  var os, arch, ext, label;

  if (/Mac/i.test(ua)) {
    os = 'darwin'; arch = 'arm64'; ext = 'tar.gz';
    label = 'macOS';
  } else if (/Win/i.test(ua)) {
    os = 'windows'; arch = 'amd64'; ext = 'zip';
    label = 'Windows';
  } else {
    os = 'linux'; arch = 'amd64'; ext = 'tar.gz';
    label = 'Linux';
  }

  var url = 'https://github.com/jinto/kittypaw/releases/latest/download/kittypaw_' + os + '_' + arch + '.' + ext;
  btn.href = url;
  btn.querySelector('span').textContent = '⬇';
  btn.childNodes[btn.childNodes.length - 1].textContent = ' Download for ' + label;
})();

/* Obfuscate — assemble split data attributes for bot protection */
document.querySelectorAll('.obf-p,.obf-a,.obf-b').forEach(function (el) {
  el.textContent = (el.dataset.a || '') + (el.dataset.b || '') + (el.dataset.c || '');
});

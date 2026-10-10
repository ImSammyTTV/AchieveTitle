// Unlock sounds shared by the OBS overlay and the settings page preview.
// The built-in ones are synthesised, so there are no sound files to ship.
(function () {
  let ctx;
  const audio = () => (ctx = ctx || new (window.AudioContext || window.webkitAudioContext)());

  function note(ac, out, freq, start, dur, type, peak) {
    const o = ac.createOscillator(), g = ac.createGain();
    o.type = type; o.frequency.value = freq;
    g.gain.setValueAtTime(0.0001, start);
    g.gain.exponentialRampToValueAtTime(peak, start + 0.015);
    g.gain.exponentialRampToValueAtTime(0.0001, start + dur);
    o.connect(g).connect(out);
    o.start(start); o.stop(start + dur + 0.05);
  }

  const built = {
    // A bright rising bell arpeggio.
    chime(ac, out) {
      const t = ac.currentTime + 0.02;
      [1046.5, 1318.5, 1568, 2093].forEach((f, i) => {
        note(ac, out, f, t + i * 0.09, 1.1, 'sine', 0.5);
        note(ac, out, f * 2, t + i * 0.09, 0.5, 'sine', 0.08); // shimmer
      });
    },
    // A short brass-style "ta-da".
    fanfare(ac, out) {
      const t = ac.currentTime + 0.02, lp = ac.createBiquadFilter();
      lp.type = 'lowpass'; lp.frequency.value = 2400; lp.connect(out);
      [[392, 0, 0.16], [523.3, 0.16, 0.16], [659.3, 0.32, 0.16], [784, 0.48, 0.9]].forEach(([f, at, d]) => {
        [f, f * 1.005, f / 2].forEach(ff => note(ac, lp, ff, t + at, d, 'sawtooth', 0.12));
      });
    },
  };

  // kind: "none" | "chime" | "fanfare" | "custom"; volume: 0-100.
  window.playUnlockSound = function (kind, volume, customUrl) {
    const vol = Math.max(0, Math.min(100, volume ?? 70)) / 100;
    if (!kind || kind === 'none' || vol === 0) return;
    if (kind === 'custom') {
      const a = new Audio(customUrl);
      a.volume = vol;
      a.play().catch(() => {});
      return;
    }
    const ac = audio();
    if (ac.state === 'suspended') ac.resume();
    const master = ac.createGain();
    master.gain.value = vol;
    master.connect(ac.destination);
    (built[kind] || built.chime)(ac, master);
  };
})();

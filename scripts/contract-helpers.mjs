export const executableName = (name, platform = process.platform) => platform === 'win32' ? `${name}.exe` : name;

export function spawnFailure(label, result) {
  return `${label} failed: ${result.error?.message ?? (result.stderr?.trim() || `exit status ${result.status}`)}`;
}

/** Keys present on the wire that schema parsing removed, including inside arrays. */
export function droppedKeys(raw, parsed, path = '$') {
  if (!raw || typeof raw !== 'object') return [];
  return Object.keys(raw).flatMap((key) => {
    const next = Array.isArray(raw) ? `${path}[${key}]` : `${path}.${key}`;
    return parsed && Object.hasOwn(parsed, key) ? droppedKeys(raw[key], parsed[key], next) : [next];
  });
}

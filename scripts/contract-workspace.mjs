import { rmSync } from 'node:fs';

/** Wait for termination before removing a possibly locked executable/database. */
async function stopServer(server, timeoutMs) {
  if (!server?.pid || server.exitCode !== null || server.signalCode !== null) return;
  await new Promise((resolve, reject) => {
    let timer;
    const finished = () => { clearTimeout(timer); resolve(); };
    server.once('exit', finished);
    timer = setTimeout(() => {
      server.kill('SIGKILL');
      timer = setTimeout(() => {
        server.removeListener('exit', finished);
        reject(new Error('server did not exit after SIGKILL; workspace retained'));
      }, timeoutMs);
    }, timeoutMs);
    server.kill('SIGTERM');
  });
}

/** Own all resources for success, errors and interruptible async work. */
export async function withContractWorkspace(work, { keep = false, timeoutMs = 2000 } = {}, run) {
  const resources = {};
  const controller = new AbortController();
  resources.abort = (error) => controller.abort(error);
  const handlers = ['SIGINT', 'SIGTERM'].map((signal) => {
    const handler = () => controller.abort(Object.assign(new Error(`interrupted by ${signal}`), {
      exitCode: signal === 'SIGINT' ? 130 : 143,
    }));
    process.on(signal, handler);
    return [signal, handler];
  });
  try {
    await run(resources, controller.signal);
    controller.signal.throwIfAborted();
  } catch (error) {
    throw controller.signal.aborted ? controller.signal.reason : error;
  } finally {
    try {
      try { resources.db?.close(); }
      finally { await stopServer(resources.server, timeoutMs); }
      if (!keep) rmSync(work, { recursive: true, force: true });
    } finally {
      for (const [signal, handler] of handlers) process.removeListener(signal, handler);
    }
  }
}

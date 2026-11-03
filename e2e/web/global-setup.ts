import { execFileSync } from 'node:child_process';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const ici = dirname(fileURLToPath(import.meta.url));

/** Démarre Keycloak de test ; E2E_NO_STACK=1 pour réutiliser une pile déjà démarrée. */
export default function globalSetup(): () => void {
  if (process.env['E2E_NO_STACK']) {
    return () => undefined;
  }
  execFileSync('docker', ['compose', '-f', 'compose.yaml', 'up', '-d', '--wait', '--wait-timeout', '180'], {
    cwd: ici,
    stdio: 'inherit',
  });
  return () => {
    execFileSync('docker', ['compose', '-f', 'compose.yaml', 'down', '-v'], { cwd: ici, stdio: 'inherit' });
  };
}

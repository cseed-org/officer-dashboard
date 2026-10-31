// Creates .dev.vars from .dev.vars.example on first `npm run dev`, so DEV_AUTH works out of the box.
import { copyFileSync, existsSync } from 'node:fs';

if (!existsSync('.dev.vars')) {
  copyFileSync('.dev.vars.example', '.dev.vars');
  console.log('Created .dev.vars from .dev.vars.example');
}

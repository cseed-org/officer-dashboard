// Prints the seed SQL to stdout. `npm run db:seed:local` pipes it into local D1.
import { buildSeedSql } from './seed/sql';

process.stdout.write(buildSeedSql());

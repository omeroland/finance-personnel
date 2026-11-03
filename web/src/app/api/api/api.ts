export * from './evaluations.service';
import { EvaluationsService } from './evaluations.service';
export * from './imports.service';
import { ImportsService } from './imports.service';
export * from './rules.service';
import { RulesService } from './rules.service';
export * from './stocks.service';
import { StocksService } from './stocks.service';
export const APIS = [EvaluationsService, ImportsService, RulesService, StocksService];

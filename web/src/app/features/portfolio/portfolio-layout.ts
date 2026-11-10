import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { TraductionPipe } from '../../i18n/t.pipe';

/** Gabarit du module 2 : titre, sous-navigation entre les six écrans, contenu de l'écran actif. */
@Component({
  selector: 'app-portfolio-layout',
  imports: [RouterLink, RouterLinkActive, RouterOutlet, TraductionPipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './portfolio.scss',
  template: `
    <h1>{{ 'pf.titre' | t }}</h1>
    <nav [attr.aria-label]="'pf.nav' | t">
      <ul class="sous-nav">
        <li>
          <a
            routerLink="."
            [routerLinkActiveOptions]="{ exact: true }"
            routerLinkActive="actif"
            ariaCurrentWhenActive="page"
            data-testid="pf-nav-tableau"
            >{{ 'pf.nav.tableau' | t }}</a
          >
        </li>
        <li>
          <a routerLink="modeles" routerLinkActive="actif" ariaCurrentWhenActive="page" data-testid="pf-nav-modeles">{{
            'pf.nav.modeles' | t
          }}</a>
        </li>
        <li>
          <a routerLink="achat" routerLinkActive="actif" ariaCurrentWhenActive="page" data-testid="pf-nav-achat">{{
            'pf.nav.achat' | t
          }}</a>
        </li>
        <li>
          <a
            routerLink="reequilibrage"
            routerLinkActive="actif"
            ariaCurrentWhenActive="page"
            data-testid="pf-nav-reequilibrage"
            >{{ 'pf.nav.reequilibrage' | t }}</a
          >
        </li>
        <li>
          <a
            routerLink="exposition"
            routerLinkActive="actif"
            ariaCurrentWhenActive="page"
            data-testid="pf-nav-exposition"
            >{{ 'pf.nav.exposition' | t }}</a
          >
        </li>
        <li>
          <a
            routerLink="rendement"
            routerLinkActive="actif"
            ariaCurrentWhenActive="page"
            data-testid="pf-nav-rendement"
            >{{ 'pf.nav.rendement' | t }}</a
          >
        </li>
      </ul>
    </nav>
    <router-outlet />
  `,
})
export class PortfolioLayout {}

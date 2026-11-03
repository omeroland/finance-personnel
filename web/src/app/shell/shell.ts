import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { MatButtonModule } from '@angular/material/button';
import { MatListModule } from '@angular/material/list';
import { MatSidenavModule } from '@angular/material/sidenav';
import { MatToolbarModule } from '@angular/material/toolbar';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { OidcSecurityService } from 'angular-auth-oidc-client';
import { TraductionPipe } from '../i18n/t.pipe';

/** Coquille multi-modules : barre supérieure, menu latéral, contenu du module actif. */
@Component({
  selector: 'app-shell',
  imports: [
    MatButtonModule,
    MatListModule,
    MatSidenavModule,
    MatToolbarModule,
    RouterLink,
    RouterLinkActive,
    RouterOutlet,
    TraductionPipe,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './shell.html',
  styleUrl: './shell.scss',
})
export class Shell {
  private readonly oidc = inject(OidcSecurityService);
  private readonly donnees = toSignal(this.oidc.userData$);

  protected readonly nom = computed(() => {
    const u = this.donnees()?.userData as Record<string, unknown> | null | undefined;
    const valeur = u?.['name'] ?? u?.['preferred_username'];
    return typeof valeur === 'string' ? valeur : '';
  });

  protected deconnexion(): void {
    this.oidc.logoff().subscribe();
  }
}

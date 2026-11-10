import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { OidcSecurityService } from 'angular-auth-oidc-client';
import { of } from 'rxjs';
import { Shell } from './shell';

describe('Shell', () => {
  const logoff = vi.fn(() => of(null));

  function creer() {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        { provide: OidcSecurityService, useValue: { userData$: of({ userData: { name: 'Test User' } }), logoff } },
      ],
    });
    const fixture = TestBed.createComponent(Shell);
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('affiche les deux modules actifs : le portefeuille de FNB mène à /portefeuille (D450)', () => {
    const el = creer();
    const m1 = el.querySelector('[data-testid="menu-stockeval"]');
    const m2 = el.querySelector('[data-testid="menu-etf"]');
    expect(m1?.textContent).toContain("Évaluation d'actions");
    expect(m1?.getAttribute('aria-disabled')).not.toBe('true');
    expect(m2?.textContent).toContain('Portefeuille de FNB');
    expect(m2?.textContent).not.toContain('phase 2');
    expect(m2?.getAttribute('aria-disabled')).not.toBe('true');
    expect(m2?.getAttribute('href')).toBe('/portefeuille');
  });

  it("affiche l'utilisateur et déconnecte au clic", () => {
    const el = creer();
    expect(el.querySelector('[data-testid="utilisateur"]')?.textContent).toContain('Test User');
    (el.querySelector('[data-testid="deconnexion"]') as HTMLButtonElement).click();
    expect(logoff).toHaveBeenCalled();
  });
});

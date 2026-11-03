import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { BASE_PATH } from '../../../api';
import { ImportPage, TAILLE_MAX_OCTETS } from './import-page';

describe('ImportPage', () => {
  function creer() {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), { provide: BASE_PATH, useValue: '' }],
    });
    const fixture = TestBed.createComponent(ImportPage);
    fixture.detectChanges();
    return fixture;
  }

  function choisir(fixture: ReturnType<typeof creer>, fichier: File): void {
    const input = fixture.nativeElement.querySelector('[data-testid="fichier"]') as HTMLInputElement;
    // jsdom n'a pas DataTransfer : on simule la liste de fichiers.
    Object.defineProperty(input, 'files', { value: { 0: fichier, length: 1, item: () => fichier }, configurable: true });
    input.dispatchEvent(new Event('change'));
    fixture.detectChanges();
  }

  it("propose l'année précédente par défaut", async () => {
    const fixture = creer();
    await fixture.whenStable();
    const annee = fixture.nativeElement.querySelector('[data-testid="annee"]') as HTMLInputElement;
    expect(annee.value).toBe(String(new Date().getFullYear() - 1));
  });

  it('refuse un fichier de plus de 10 Mo sans envoyer de requête', () => {
    const fixture = creer();
    const http = TestBed.inject(HttpTestingController);
    choisir(fixture, new File([new Uint8Array(TAILLE_MAX_OCTETS + 1)], 'gros.xlsx'));
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('[data-testid="erreur-fichier"]')?.textContent).toContain('10 Mo');
    expect((el.querySelector('[data-testid="envoyer"]') as HTMLButtonElement).disabled).toBe(true);
    http.expectNone(() => true);
  });

  it('envoie un fichier valide et affiche le compte rendu', () => {
    const fixture = creer();
    const http = TestBed.inject(HttpTestingController);
    choisir(fixture, new File(['x'], 'feuille.xlsx'));
    const bouton = fixture.nativeElement.querySelector('[data-testid="envoyer"]') as HTMLButtonElement;
    expect(bouton.disabled).toBe(false);
    bouton.click();
    const req = http.expectOne('/api/v1/imports/xlsx');
    expect(req.request.method).toBe('POST');
    req.flush({ id: '1', type: 'xlsx', status: 'succeeded', createdAt: '2026-01-01T00:00:00Z', entries: [] });
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('[data-testid="rapport"]')).not.toBeNull();
  });
});

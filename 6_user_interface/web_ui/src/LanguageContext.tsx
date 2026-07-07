/**
 * @file LanguageContext.tsx
 * @brief Context quản lý ngôn ngữ — ISO 639-1 Compliant
 * @tính_năng:
 *   - Mã ngôn ngữ chuẩn: 'vi' | 'en'
 *   - Tự động cập nhật <html lang="..."> khi chuyển ngôn ngữ
 *   - Hàm getLocale() trả về locale chuẩn cho date/number formatting
 *   - Type-safe: `t` có kiểu TranslationKeys thay vì `any`
 */

import { createContext, useContext, useState, useEffect } from 'react';
import type { ReactNode } from 'react';
import { translations } from './translations';
import type { TranslationKeys } from './translations';

export type LangType = 'vi' | 'en';

interface LanguageContextProps {
  lang: LangType;
  setLang: (lang: LangType) => void;
  t: TranslationKeys;
  /** Trả về locale chuẩn BCP-47 tương ứng (vd: 'vi-VN', 'en-US') */
  getLocale: () => string;
}

const LanguageContext = createContext<LanguageContextProps>({
  lang: 'vi',
  setLang: () => {},
  t: translations['vi'],
  getLocale: () => 'vi-VN',
});

export const LanguageProvider = ({ children }: { children: ReactNode }) => {
  const [lang, setLangState] = useState<LangType>(() => {
    const saved = localStorage.getItem('vanguard_lang');
    return (saved === 'vi' || saved === 'en') ? saved : 'vi';
  });

  // Khi ngôn ngữ thay đổi → cập nhật <html lang="..."> cho SEO + screen readers + lưu vào localStorage
  const setLang = (newLang: LangType) => {
    setLangState(newLang);
    document.documentElement.lang = newLang;
    localStorage.setItem('vanguard_lang', newLang);
  };

  // Đồng bộ lang attribute lần đầu khi mount
  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  const getLocale = (): string => {
    return lang === 'vi' ? 'vi-VN' : 'en-US';
  };

  return (
    <LanguageContext.Provider value={{ lang, setLang, t: translations[lang], getLocale }}>
      {children}
    </LanguageContext.Provider>
  );
};

export const useLanguage = () => useContext(LanguageContext);

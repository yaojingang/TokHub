import i18n from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";
import { resources, supportedLanguages, type SupportedLanguage } from "./locales";

const defaultLanguage: SupportedLanguage = "zh-CN";

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: defaultLanguage,
    supportedLngs: [...supportedLanguages],
    defaultNS: "common",
    ns: ["common", "admin", "console", "public"],
    interpolation: {
      escapeValue: false
    },
    detection: {
      order: ["querystring", "localStorage"],
      lookupQuerystring: "lng",
      lookupLocalStorage: "tokhub.lng",
      caches: ["localStorage"]
    },
    react: {
      useSuspense: false
    }
  });

export { i18n };
export type { SupportedLanguage };

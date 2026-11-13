/**
 * "Hello, welcome to cseed" in 15 languages. `{cseed}` marks the club name, which is always
 * the lowercase Latin word whatever the sentence around it (it gets its own LTR span).
 * `dir` is only set for right-to-left scripts.
 */
export interface Greeting {
  lang: string;
  name: string;
  text: string;
  dir?: 'rtl';
}

export const GREETINGS: Greeting[] = [
  { lang: 'en', name: 'English', text: 'Hello, welcome to {cseed}' },
  { lang: 'es', name: 'Spanish', text: 'Hola, bienvenido a {cseed}' },
  { lang: 'zh-Hans', name: 'Mandarin', text: '你好，欢迎来到 {cseed}' },
  { lang: 'hi', name: 'Hindi', text: 'नमस्ते, {cseed} में आपका स्वागत है' },
  { lang: 'ar', name: 'Arabic', text: 'مرحباً، أهلاً بك في {cseed}', dir: 'rtl' },
  { lang: 'fr', name: 'French', text: 'Bonjour, bienvenue chez {cseed}' },
  { lang: 'pt', name: 'Portuguese', text: 'Olá, bem-vindo ao {cseed}' },
  { lang: 'ja', name: 'Japanese', text: 'こんにちは、{cseed}へようこそ' },
  { lang: 'ko', name: 'Korean', text: '안녕하세요, {cseed}에 오신 것을 환영합니다' },
  { lang: 'de', name: 'German', text: 'Hallo, willkommen bei {cseed}' },
  { lang: 'vi', name: 'Vietnamese', text: 'Xin chào, chào mừng bạn đến với {cseed}' },
  { lang: 'fil', name: 'Tagalog', text: 'Kumusta, maligayang pagdating sa {cseed}' },
  { lang: 'ru', name: 'Russian', text: 'Привет, добро пожаловать в {cseed}' },
  { lang: 'sw', name: 'Swahili', text: 'Hujambo, karibu kwenye {cseed}' },
  { lang: 'it', name: 'Italian', text: 'Ciao, benvenuto su {cseed}' },
];

/** The sentence a screen reader gets, once, instead of fifteen cycling ones. */
export const ACCESSIBLE_GREETING = 'Hello, welcome to cseed';

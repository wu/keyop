import {formatAge} from '/js/time-formatter.js';

describe('formatAge', () => {
    // Mock Date.now() for consistent testing
    let originalDate;
    let mockNow;

    beforeEach(() => {
        originalDate = Date.now;
        mockNow = new Date('2026-03-14T20:00:00Z').getTime();
        Date.now = () => mockNow;
    });

    afterEach(() => {
        Date.now = originalDate;
    });

    describe('past times', () => {
        test('returns "just now" for times less than a minute ago', () => {
            const thirtySecondsAgo = new Date(mockNow - 30 * 1000).toISOString();
            expect(formatAge(thirtySecondsAgo)).toBe('just now');
        });

        test('returns minutes for times 1-59 minutes ago', () => {
            const fiveMinutesAgo = new Date(mockNow - 5 * 60 * 1000).toISOString();
            expect(formatAge(fiveMinutesAgo)).toBe('5m ago');
        });

        test('returns hours and minutes for times 1-23 hours ago', () => {
            const twoHoursThirtyMinutesAgo = new Date(mockNow - 2.5 * 60 * 60 * 1000).toISOString();
            expect(formatAge(twoHoursThirtyMinutesAgo)).toBe('2h 30m ago');
        });

        test('returns days and hours for times 1-6 days ago', () => {
            const threeDaysFiveHoursAgo = new Date(mockNow - (3 * 24 + 5) * 60 * 60 * 1000).toISOString();
            expect(formatAge(threeDaysFiveHoursAgo)).toBe('3d 5h ago');
        });

        test('returns weeks and days for times 1-4 weeks ago', () => {
            const twoWeeksThreeDaysAgo = new Date(mockNow - (14 + 3) * 24 * 60 * 60 * 1000).toISOString();
            expect(formatAge(twoWeeksThreeDaysAgo)).toBe('2w 3d ago');
        });

        test('returns months and days for times 1-11 months ago', () => {
            const fourMonthsFiveDaysAgo = new Date(mockNow - (120 + 5) * 24 * 60 * 60 * 1000).toISOString();
            expect(formatAge(fourMonthsFiveDaysAgo)).toBe('4m 5d ago');
        });

        test('returns years and months for times 1+ years ago', () => {
            const twoYearsThreeMonthsAgo = new Date(mockNow - (730 + 90) * 24 * 60 * 60 * 1000).toISOString();
            expect(formatAge(twoYearsThreeMonthsAgo)).toBe('2y 3m ago');
        });
    });

    describe('future times', () => {
        test('returns "in seconds" for times less than a minute away', () => {
            const thirtySecondsFromNow = new Date(mockNow + 30 * 1000).toISOString();
            expect(formatAge(thirtySecondsFromNow)).toBe('in seconds');
        });

        test('returns minutes for times 1-59 minutes away', () => {
            const fiveMinutesFromNow = new Date(mockNow + 5 * 60 * 1000).toISOString();
            expect(formatAge(fiveMinutesFromNow)).toBe('in 5m');
        });

        test('returns hours and minutes for times 1-23 hours away', () => {
            const twoHoursThirtyMinutesFromNow = new Date(mockNow + 2.5 * 60 * 60 * 1000).toISOString();
            expect(formatAge(twoHoursThirtyMinutesFromNow)).toBe('in 2h 30m');
        });

        test('returns days and hours for times 1-6 days away', () => {
            const threeDaysFiveHoursFromNow = new Date(mockNow + (3 * 24 + 5) * 60 * 60 * 1000).toISOString();
            expect(formatAge(threeDaysFiveHoursFromNow)).toBe('in 3d 5h');
        });

        test('returns weeks and days for times 1-4 weeks away', () => {
            const twoWeeksThreeDaysFromNow = new Date(mockNow + (14 + 3) * 24 * 60 * 60 * 1000).toISOString();
            expect(formatAge(twoWeeksThreeDaysFromNow)).toBe('in 2w 3d');
        });

        test('returns months and days for times 1-11 months away', () => {
            const fourMonthsFiveDaysFromNow = new Date(mockNow + (120 + 5) * 24 * 60 * 60 * 1000).toISOString();
            expect(formatAge(fourMonthsFiveDaysFromNow)).toBe('in 4m 5d');
        });

        test('returns years and months for times 1+ years away', () => {
            const twoYearsThreeMonthsFromNow = new Date(mockNow + (730 + 90) * 24 * 60 * 60 * 1000).toISOString();
            expect(formatAge(twoYearsThreeMonthsFromNow)).toBe('in 2y 3m');
        });
    });

    describe('edge cases', () => {
        test('returns "never" for null/undefined input', () => {
            expect(formatAge(null)).toBe('never');
            expect(formatAge(undefined)).toBe('never');
        });

        test('handles empty string as "never"', () => {
            expect(formatAge('')).toBe('never');
        });
    });
});

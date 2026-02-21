/**
 * Thai Buddhist Era (BE) Date Picker
 * ===================================
 * Flatpickr wrapper that displays Thai Buddhist Era year (CE + 543).
 * 
 * Dependencies: flatpickr, flatpickr/l10n/th.js
 * 
 * Usage:
 *   <input type="text" id="myDate" placeholder="Select date" readonly>
 *   <script src="/static/js/thai_custom_date.js"></script>
 *   <script>
 *     var picker = ThaiDatePicker.init('#myDate');
 *     picker.getDate()   // returns AD date string "2025-02-21"
 *     picker.getBEDate() // returns "21 กุมภาพันธ์ 2568"
 *     picker.clear()
 *     picker.setDate('2025-01-15')
 *   </script>
 * 
 * Multiple pickers:
 *   ThaiDatePicker.initAll('.thai-date');
 * 
 * @version 1.0.0
 * @author PWA GIS Team
 */

var ThaiDatePicker = (function() {
    'use strict';

    // Full Thai month names
    var TH_MONTHS = [
        "มกราคม", "กุมภาพันธ์", "มีนาคม", "เมษายน",
        "พฤษภาคม", "มิถุนายน", "กรกฎาคม", "สิงหาคม",
        "กันยายน", "ตุลาคม", "พฤศจิกายน", "ธันวาคม"
    ];

    // Abbreviated Thai month names
    var TH_MONTHS_SHORT = [
        "ม.ค.", "ก.พ.", "มี.ค.", "เม.ย.",
        "พ.ค.", "มิ.ย.", "ก.ค.", "ส.ค.",
        "ก.ย.", "ต.ค.", "พ.ย.", "ธ.ค."
    ];

    // Abbreviated Thai day names
    var TH_DAYS_SHORT = ["อา", "จ", "อ", "พ", "พฤ", "ศ", "ส"];

    /**
     * Patches the calendar header to display Buddhist Era year (CE + 543).
     * @param {Object} fp - flatpickr instance
     */
    function patchBE(fp) {
        requestAnimationFrame(function() {
            if (!fp.calendarContainer) return;

            // Replace year display with BE year
            var yearInput = fp.calendarContainer.querySelector('.cur-year');
            if (yearInput) {
                yearInput.value = fp.currentYear + 543;
                yearInput.readOnly = true;
                yearInput.style.pointerEvents = 'none';
            }

            // Replace month label with Thai month name
            var monthEl = fp.calendarContainer.querySelector('.flatpickr-current-month .cur-month');
            if (monthEl) {
                monthEl.textContent = TH_MONTHS[fp.currentMonth];
            }
        });
    }

    /**
     * Patches the alternative input to display the BE formatted date.
     * @param {Object} fp - flatpickr instance
     * @param {string} format - 'full' or 'short'
     */
    function patchAltInput(fp, format) {
        if (!fp.altInput || !fp.selectedDates || !fp.selectedDates.length) return;

        var d = fp.selectedDates[0];
        var day = d.getDate();
        var month = d.getMonth();
        var beYear = d.getFullYear() + 543;

        var formatted;
        switch (format) {
            case 'short':
                formatted = day + ' ' + TH_MONTHS_SHORT[month] + ' ' + beYear;
                break;
            case 'full':
            default:
                formatted = day + ' ' + TH_MONTHS[month] + ' ' + beYear;
                break;
        }

        fp.altInput.value = formatted;
    }

    /**
     * Initializes a Thai BE date picker on the given selector.
     * 
     * @param {string|Element} selector - CSS selector or DOM element
     * @param {Object} [options] - Configuration options
     * @param {string} [options.format='full'] - Display format: 'full' or 'short'
     * @param {string} [options.defaultDate] - Default date in AD format (e.g. '2025-01-01')
     * @param {string} [options.minDate] - Minimum selectable date
     * @param {string} [options.maxDate] - Maximum selectable date
     * @param {Function} [options.onChange] - Callback when date changes (dateStr, dateObj)
     * @returns {Object|null} Wrapper object with helper methods, or null if flatpickr is not loaded
     */
    function init(selector, options) {
        if (!window.flatpickr) {
            console.warn('ThaiDatePicker: flatpickr is not loaded');
            return null;
        }

        options = options || {};
        var displayFormat = options.format || 'full';

        // Apply Thai locale if available
        if (flatpickr.l10ns && flatpickr.l10ns.th) {
            flatpickr.localize(flatpickr.l10ns.th);
        }

        var fpConfig = {
            locale: "th",
            dateFormat: "Y-m-d",       // Internal value in AD format (for API calls)
            altInput: true,
            altFormat: "j F Y",
            allowInput: false,
            onReady: function(_, __, fp) { patchBE(fp); },
            onMonthChange: function(_, __, fp) { patchBE(fp); },
            onYearChange: function(_, __, fp) { patchBE(fp); },
            onChange: function(selectedDates, dateStr, fp) {
                patchBE(fp);
                patchAltInput(fp, displayFormat);
                if (options.onChange) {
                    options.onChange(dateStr, selectedDates[0]);
                }
            },
            onOpen: function(_, __, fp) { patchBE(fp); }
        };

        // Apply optional configurations
        if (options.defaultDate) fpConfig.defaultDate = options.defaultDate;
        if (options.minDate) fpConfig.minDate = options.minDate;
        if (options.maxDate) fpConfig.maxDate = options.maxDate;

        var fp = flatpickr(selector, fpConfig);

        // Return wrapper object with convenience methods
        return {
            fp: fp,

            /** Returns the AD date string (YYYY-MM-DD) or empty string */
            getDate: function() {
                if (fp.selectedDates && fp.selectedDates.length) {
                    return fp.formatDate(fp.selectedDates[0], 'Y-m-d');
                }
                return '';
            },

            /** Returns the Thai BE formatted date string */
            getBEDate: function() {
                if (!fp.selectedDates || !fp.selectedDates.length) return '';
                var d = fp.selectedDates[0];
                return d.getDate() + ' ' + TH_MONTHS[d.getMonth()] + ' ' + (d.getFullYear() + 543);
            },

            /** Returns the raw Date object or null */
            getDateObj: function() {
                return fp.selectedDates && fp.selectedDates.length ? fp.selectedDates[0] : null;
            },

            /** Sets the date from an AD string (e.g. '2025-01-15') */
            setDate: function(dateStr, triggerChange) {
                fp.setDate(dateStr, triggerChange !== false);
            },

            /** Clears the selected date */
            clear: function() {
                fp.clear();
            },

            /** Destroys the picker instance */
            destroy: function() {
                fp.destroy();
            }
        };
    }

    /**
     * Initializes Thai BE pickers on all elements matching the selector.
     * @param {string} selector - CSS selector
     * @param {Object} [options] - Same options as init()
     * @returns {Array} Array of wrapper objects
     */
    function initAll(selector, options) {
        var elements = document.querySelectorAll(selector);
        var pickers = [];
        elements.forEach(function(el) {
            pickers.push(init(el, options));
        });
        return pickers;
    }

    /**
     * Formats a Date or date string to Thai Buddhist Era format.
     * @param {Date|string} date - Date to format
     * @param {string} [format='full'] - 'full' or 'short'
     * @returns {string} Formatted date string
     */
    function formatBE(date, format) {
        if (!date) return '';
        if (typeof date === 'string') date = new Date(date);
        if (isNaN(date.getTime())) return '';

        var day = date.getDate();
        var month = date.getMonth();
        var beYear = date.getFullYear() + 543;

        if (format === 'short') {
            return day + ' ' + TH_MONTHS_SHORT[month] + ' ' + beYear;
        }
        return day + ' ' + TH_MONTHS[month] + ' ' + beYear;
    }

    /**
     * Returns today's date formatted in Thai BE.
     * @param {string} [format='full'] - 'full' or 'short'
     * @returns {string}
     */
    function todayBE(format) {
        return formatBE(new Date(), format);
    }

    // Public API
    return {
        init: init,
        initAll: initAll,
        formatBE: formatBE,
        todayBE: todayBE,
        TH_MONTHS: TH_MONTHS,
        TH_MONTHS_SHORT: TH_MONTHS_SHORT,
        TH_DAYS_SHORT: TH_DAYS_SHORT
    };

})();
